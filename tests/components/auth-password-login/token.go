package main

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// minSecretBytes 是签名密钥的最小长度。
//
// HS256 的安全性**完全等于**密钥的强度：密钥被暴力破解，攻击者就能签出任意
// 身份的令牌，而且不会在任何地方留下痕迹。32 字节对应 HMAC-SHA256 的输出长度，
// 是 RFC 8725 §3.5 给出的下限。
const minSecretBytes = 32

// subject 是要写进令牌的身份。
//
// 只放"下游判断权限时用得上、且变动很慢"的信息。令牌一旦签出去，在过期之前
// 就改不了了——把频繁变动的东西放进去，等于发出一堆很快就不准的快照。
type subject struct {
	PersonID     string
	Username     string
	DepartmentID string
}

// claims 是本组件签发的令牌载荷。
//
// 载荷只是 base64，**不是加密**：任何拿到令牌的人都能读。因此这里不放口令、
// 不放哈希、不放任何秘密（token_test.go 里有专门的用例锁这一条）。
type claims struct {
	jwt.RegisteredClaims

	Username     string `json:"username,omitempty"`
	DepartmentID string `json:"departmentId,omitempty"`
}

// tokenIssuer 负责签发与校验令牌。
type tokenIssuer struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// newTokenIssuer 构造签发器，并在这里就把弱配置挡掉。
//
// 挡在构造函数而不是运行时：一个用弱密钥跑起来的认证组件，表面上一切正常，
// 只有被攻破时才会发现。宁可根本起不来。
func newTokenIssuer(secret string, ttl time.Duration, now func() time.Time) (*tokenIssuer, error) {
	if len(secret) < minSecretBytes {
		return nil, errors.New("JWT 签名密钥太短：至少需要 " +
			itoa(minSecretBytes) + " 字节，当前 " + itoa(len(secret)) +
			" 字节（HS256 的安全性完全取决于密钥强度，见 RFC 8725 §3.5）")
	}
	if ttl <= 0 {
		return nil, errors.New("令牌有效期必须为正数：签出来就已经过期的令牌毫无意义")
	}
	if now == nil {
		now = time.Now
	}
	return &tokenIssuer{secret: []byte(secret), ttl: ttl, now: now}, nil
}

// signingMethod 是本组件唯一接受的算法。
//
// 定死在这里，是为了让校验时**不看令牌自称的算法**——那正是 alg=none
// 与算法混淆攻击的入口（RFC 8725 §3.1）。
var signingMethod = jwt.SigningMethodHS256

// issue 为一个主体签发令牌。
func (i *tokenIssuer) issue(s subject) (string, error) {
	issuedAt := i.now()

	token := jwt.NewWithClaims(signingMethod, claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   s.PersonID,
			IssuedAt:  jwt.NewNumericDate(issuedAt),
			ExpiresAt: jwt.NewNumericDate(issuedAt.Add(i.ttl)),
			NotBefore: jwt.NewNumericDate(issuedAt),
		},
		Username:     s.Username,
		DepartmentID: s.DepartmentID,
	})
	return token.SignedString(i.secret)
}

// expiresAt 返回此刻签发的令牌会在什么时候过期，供响应体使用。
func (i *tokenIssuer) expiresAt() time.Time { return i.now().Add(i.ttl) }

// parse 校验并解析令牌。
//
// 三道关，缺一不可：
//  1. 只接受 HS256——不看令牌自称的算法（挡 alg=none 与算法混淆）
//  2. 签名必须对得上——挡篡改与伪造
//  3. 时间必须在有效期内——挡过期重放
func (i *tokenIssuer) parse(raw string) (*claims, error) {
	parsed, err := jwt.ParseWithClaims(raw, &claims{},
		func(*jwt.Token) (any, error) { return i.secret, nil },
		jwt.WithValidMethods([]string{signingMethod.Alg()}),
		jwt.WithTimeFunc(i.now),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return nil, err
	}

	c, ok := parsed.Claims.(*claims)
	if !ok || !parsed.Valid {
		return nil, errors.New("令牌无效")
	}
	if c.Subject == "" {
		// 没有 sub 的令牌说明不了"这是谁"，认了等于认了个匿名身份
		return nil, errors.New("令牌缺少 sub")
	}
	return c, nil
}

// itoa 是一个不引入 strconv 的小工具（只用于错误文案里的小整数）。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
