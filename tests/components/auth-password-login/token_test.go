package main

import (
	"strings"
	"testing"
	"time"
)

// 本文件覆盖开发计划 23.2（JWT 格式正确：包含 exp/iat/sub），
// 以及一组"签发方最容易被绕过"的攻击面。
//
// 令牌是这个组件唯一的产出物。它被签发出去之后，组件就管不着了——
// 别人拿着它去调别的服务。因此这里的每一条都不是"锦上添花"，
// 而是"错了就等于没有认证"。

const (
	testSecret = "test-secret-at-least-32-bytes-long!!"
	testTTL    = 30 * time.Minute
)

// fixedClock 让令牌测试不依赖真实时钟。
func fixedClock(ts string) func() time.Time {
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		panic(err)
	}
	return func() time.Time { return t }
}

func newTestIssuer(t *testing.T, now func() time.Time) *tokenIssuer {
	t.Helper()

	issuer, err := newTokenIssuer(testSecret, testTTL, now)
	if err != nil {
		t.Fatalf("构造签发器失败：%v", err)
	}
	return issuer
}

// ============================================================
// 23.2 JWT 格式
// ============================================================

func TestIssuedTokenCarriesRequiredClaims(t *testing.T) {
	now := fixedClock("2026-01-01T10:00:00Z")
	issuer := newTestIssuer(t, now)

	token, err := issuer.issue(subject{PersonID: "p-1", Username: "zhangsan", DepartmentID: "d-tech"})
	if err != nil {
		t.Fatalf("签发失败：%v", err)
	}
	if strings.Count(token, ".") != 2 {
		t.Fatalf("JWT 应当是三段点分结构，实际：%q", token)
	}

	claims, err := issuer.parse(token)
	if err != nil {
		t.Fatalf("解析自己签的令牌都失败：%v", err)
	}

	if claims.Subject != "p-1" {
		t.Errorf("sub 应当是 personId p-1，实际 %q", claims.Subject)
	}
	if claims.IssuedAt == nil || !claims.IssuedAt.Time.Equal(now()) {
		t.Errorf("iat 应当是签发时刻 %v，实际 %v", now(), claims.IssuedAt)
	}
	if claims.ExpiresAt == nil || !claims.ExpiresAt.Time.Equal(now().Add(testTTL)) {
		t.Errorf("exp 应当是 iat + TTL = %v，实际 %v", now().Add(testTTL), claims.ExpiresAt)
	}
	if claims.Username != "zhangsan" || claims.DepartmentID != "d-tech" {
		t.Errorf("自定义 claim 丢失：%+v", claims)
	}
}

// TestTokenNeverCarriesSecrets：令牌是给别人看的，里面不能有秘密。
//
// JWT 的载荷只是 base64，**不是加密**。把口令哈希放进去等于公开发给所有人。
func TestTokenNeverCarriesSecrets(t *testing.T) {
	issuer := newTestIssuer(t, fixedClock("2026-01-01T10:00:00Z"))

	token, err := issuer.issue(subject{PersonID: "p-1", Username: "zhangsan"})
	if err != nil {
		t.Fatalf("签发失败：%v", err)
	}

	for _, forbidden := range []string{"password", "hash", "secret", testSecret} {
		if strings.Contains(strings.ToLower(token), strings.ToLower(forbidden)) {
			t.Errorf("令牌里出现了不该出现的内容：%q", forbidden)
		}
	}
}

// ============================================================
// 校验侧：这些都不是理论攻击
// ============================================================

// TestParseRejectsForeignSignature：别人用自己的密钥签的令牌不能认。
func TestParseRejectsForeignSignature(t *testing.T) {
	now := fixedClock("2026-01-01T10:00:00Z")
	attacker, err := newTokenIssuer("attacker-secret-at-least-32-bytes!!!", testTTL, now)
	if err != nil {
		t.Fatalf("构造攻击者签发器失败：%v", err)
	}

	forged, err := attacker.issue(subject{PersonID: "p-admin", Username: "admin"})
	if err != nil {
		t.Fatalf("签发失败：%v", err)
	}

	if _, err := newTestIssuer(t, now).parse(forged); err == nil {
		t.Fatal("用别的密钥签出来的令牌必须被拒绝")
	}
}

// TestParseRejectsAlgNone 是 JWT 最经典的一个洞。
//
// 攻击者把 header 的 alg 改成 "none"、把签名段清空；解析器若"按令牌自称的算法"
// 去校验，就会认为"none 算法不需要签名"从而放行——任何人都能伪造任意身份。
// 必须**只认我们自己选定的算法**，而不是令牌说什么就信什么。
func TestParseRejectsAlgNone(t *testing.T) {
	// {"alg":"none","typ":"JWT"} . {"sub":"p-admin","exp":4102444800} . （空签名）
	forged := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." +
		"eyJzdWIiOiJwLWFkbWluIiwiZXhwIjo0MTAyNDQ0ODAwfQ."

	if _, err := newTestIssuer(t, fixedClock("2026-01-01T10:00:00Z")).parse(forged); err == nil {
		t.Fatal("alg=none 的令牌必须被拒绝")
	}
}

// TestParseRejectsTamperedPayload：改了载荷、签名没跟着改。
func TestParseRejectsTamperedPayload(t *testing.T) {
	issuer := newTestIssuer(t, fixedClock("2026-01-01T10:00:00Z"))

	token, err := issuer.issue(subject{PersonID: "p-1", Username: "zhangsan"})
	if err != nil {
		t.Fatalf("签发失败：%v", err)
	}

	parts := strings.Split(token, ".")
	// 换掉 payload 段：{"sub":"p-admin","exp":4102444800}
	parts[1] = "eyJzdWIiOiJwLWFkbWluIiwiZXhwIjo0MTAyNDQ0ODAwfQ"

	if _, err := issuer.parse(strings.Join(parts, ".")); err == nil {
		t.Fatal("载荷被改过的令牌必须被拒绝")
	}
}

// TestParseRejectsExpiredToken：过了 exp 就是不认。
func TestParseRejectsExpiredToken(t *testing.T) {
	issued := fixedClock("2026-01-01T10:00:00Z")
	issuer := newTestIssuer(t, issued)

	token, err := issuer.issue(subject{PersonID: "p-1", Username: "zhangsan"})
	if err != nil {
		t.Fatalf("签发失败：%v", err)
	}

	// 把时钟拨到 TTL 之后
	expired := newTestIssuer(t, fixedClock("2026-01-01T11:00:00Z"))
	if _, err := expired.parse(token); err == nil {
		t.Fatal("过期令牌必须被拒绝")
	}

	// 边界：正好在 exp 之前仍然有效
	stillValid := newTestIssuer(t, fixedClock("2026-01-01T10:29:59Z"))
	if _, err := stillValid.parse(token); err != nil {
		t.Fatalf("尚未过期的令牌不该被拒绝：%v", err)
	}
}

// ============================================================
// 密钥本身
// ============================================================

// TestIssuerRejectsWeakSecret：密钥太短就不让启动。
//
// HS256 的安全性完全等于密钥的强度。一个 8 位的密钥可以离线暴力破解，
// 破了就能签出任意身份的令牌——而且没有任何日志会显示这件事发生过。
// 与其在运行时"看起来一切正常"，不如在启动时就不让它起来。
func TestIssuerRejectsWeakSecret(t *testing.T) {
	for name, secret := range map[string]string{
		"空":     "",
		"太短":    "short",
		"刚好差一位": strings.Repeat("a", minSecretBytes-1),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := newTokenIssuer(secret, testTTL, time.Now); err == nil {
				t.Fatalf("弱密钥 %q 必须被拒绝", secret)
			}
		})
	}

	if _, err := newTokenIssuer(strings.Repeat("a", minSecretBytes), testTTL, time.Now); err != nil {
		t.Fatalf("达到长度要求的密钥不该被拒绝：%v", err)
	}
}

func TestIssuerRejectsNonPositiveTTL(t *testing.T) {
	for _, ttl := range []time.Duration{0, -time.Second} {
		if _, err := newTokenIssuer(testSecret, ttl, time.Now); err == nil {
			t.Fatalf("TTL=%v 必须被拒绝——签出来就过期的令牌毫无意义", ttl)
		}
	}
}
