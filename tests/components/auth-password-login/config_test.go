package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 本文件覆盖 002 §1.4（配置只从环境变量读）与 §11（JSON 日志、敏感字段脱敏）。

// envOf 把 map 变成 lookup 函数。
func envOf(pairs map[string]string) func(string) string {
	return func(key string) string { return pairs[key] }
}

// completeEnv 是一份能启动的最小环境。
func completeEnv() map[string]string {
	return map[string]string{
		"DATABASE_HOST":         "postgres",
		"DATABASE_NAME":         "auth",
		"DATABASE_USER":         "auth_user",
		"DATABASE_PASSWORD":     "s3cr3t-p@ss/word",
		"PEOPLE_BASIC_ENDPOINT": "http://people-basic-1-0-0:8080",
		"JWT_SECRET":            testSecret,
	}
}

func TestConfigFromEnv(t *testing.T) {
	cfg, err := configFromEnv(envOf(completeEnv()))
	if err != nil {
		t.Fatalf("配置应当可用：%v", err)
	}

	if cfg.Database.Port != 5432 {
		t.Errorf("DATABASE_PORT 缺省应为 5432，实际 %d", cfg.Database.Port)
	}
	if cfg.PeopleEndpoint != "http://people-basic-1-0-0:8080" {
		t.Errorf("强依赖地址读取错误：%q", cfg.PeopleEndpoint)
	}
	if cfg.TokenTTL != defaultTokenTTL {
		t.Errorf("TTL 缺省应为 %v，实际 %v", defaultTokenTTL, cfg.TokenTTL)
	}
}

// TestConfigReportsAllMissingAtOnce：缺哪些一次说完。
//
// 一次报一个的话，运维要在"改配置 → 重启 → 看日志"之间来回好几轮，
// 每轮都要等容器起来。
func TestConfigReportsAllMissingAtOnce(t *testing.T) {
	_, err := configFromEnv(envOf(map[string]string{}))
	if err == nil {
		t.Fatal("什么都没配还能启动？")
	}

	for _, name := range []string{
		"DATABASE_HOST", "DATABASE_NAME", "DATABASE_USER",
		"PEOPLE_BASIC_ENDPOINT", "JWT_SECRET",
	} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("错误信息里没提到缺少 %s：%v", name, err)
		}
	}
	// 要说清楚这些变量是谁负责给的，否则使用者不知道该去改哪儿
	if !strings.Contains(err.Error(), "平台") {
		t.Error("错误信息要说明 DATABASE_* / PEOPLE_BASIC_ENDPOINT 由平台注入")
	}
}

// TestConfigNeverFallsBackToDefaults 是这个组件最不能出错的一条。
//
// 缺 JWT_SECRET 时若回落到某个内置默认密钥，所有装了这个组件的人就共用同一把
// 钥匙——任何人都能给任何一处部署签出任意身份的令牌，而且一切看起来完全正常。
// 缺数据库地址时若回落到 localhost，则是悄悄连到一个根本不对的库。
func TestConfigNeverFallsBackToDefaults(t *testing.T) {
	for _, key := range []string{"JWT_SECRET", "DATABASE_HOST", "PEOPLE_BASIC_ENDPOINT"} {
		t.Run(key, func(t *testing.T) {
			env := completeEnv()
			delete(env, key)

			if _, err := configFromEnv(envOf(env)); err == nil {
				t.Fatalf("缺少 %s 必须启动失败，绝不能用默认值顶上", key)
			}
		})
	}
}

func TestConfigRejectsBadNumbers(t *testing.T) {
	for name, pair := range map[string][2]string{
		"端口不是数字":   {"DATABASE_PORT", "abc"},
		"TTL 不是数字": {"TOKEN_TTL_SECONDS", "half-an-hour"},
		"TTL 为 0":  {"TOKEN_TTL_SECONDS", "0"},
		"TTL 为负数":  {"TOKEN_TTL_SECONDS", "-60"},
	} {
		t.Run(name, func(t *testing.T) {
			env := completeEnv()
			env[pair[0]] = pair[1]

			if _, err := configFromEnv(envOf(env)); err == nil {
				t.Fatalf("%s=%q 必须被拒绝", pair[0], pair[1])
			}
		})
	}
}

func TestTokenTTLOverride(t *testing.T) {
	env := completeEnv()
	env["TOKEN_TTL_SECONDS"] = "3600"

	cfg, err := configFromEnv(envOf(env))
	if err != nil {
		t.Fatalf("配置应当可用：%v", err)
	}
	if cfg.TokenTTL != time.Hour {
		t.Errorf("TTL 应为 1 小时，实际 %v", cfg.TokenTTL)
	}
}

// TestConfigStringHasNoSecrets：配置摘要会被打进日志，里面不能有秘密。
func TestConfigStringHasNoSecrets(t *testing.T) {
	cfg, err := configFromEnv(envOf(completeEnv()))
	if err != nil {
		t.Fatalf("配置应当可用：%v", err)
	}

	summary := cfg.String()
	for _, secret := range []string{"s3cr3t-p@ss/word", testSecret} {
		if strings.Contains(summary, secret) {
			t.Errorf("配置摘要里出现了秘密 %q：%s", secret, summary)
		}
	}
	// 但该有的定位信息要在，否则这行日志就没用了
	for _, want := range []string{"postgres", "auth", "people-basic"} {
		if !strings.Contains(summary, want) {
			t.Errorf("配置摘要里缺少 %q：%s", want, summary)
		}
	}
}

// TestDSNHandlesSpecialCharacters：强口令里的 @ : / 不能把 DSN 拆坏。
func TestDSNHandlesSpecialCharacters(t *testing.T) {
	cfg, err := configFromEnv(envOf(completeEnv()))
	if err != nil {
		t.Fatalf("配置应当可用：%v", err)
	}

	dsn := cfg.Database.DSN()
	if !strings.Contains(dsn, "@postgres:5432/auth") {
		t.Errorf("口令里的特殊字符把 DSN 拆到了错误的主机上：%s", dsn)
	}
}

// ============================================================
// 日志
// ============================================================

func TestLoggerOutputsJSONWithComponentID(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf, "info", "auth/password-login").Info("组件已就绪")

	var entry map[string]any
	if err := json.Unmarshal(buf.Bytes(), &entry); err != nil {
		t.Fatalf("日志不是 JSON：%s", buf.String())
	}
	if entry["componentId"] != "auth/password-login" {
		t.Errorf("每条日志都要带 componentId：%v", entry)
	}
}

// TestLoggerRedactsSecrets 是认证组件的一条底线。
//
// 出错时最想打印的就是"收到的请求体"，而那里面正好是明文口令。
// 所以在日志出口统一挡掉——哪怕某天有人顺手加了一行 log("password", pw)。
func TestLoggerRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf, "info", "auth/password-login").Info("登录失败",
		"password", "correct-horse-battery",
		"token", "eyJhbGciOi...",
		"jwt_secret", testSecret,
		"passwordHash", "pbkdf2-sha256$600000$...",
		"username", "zhangsan", // 这个不是秘密，要留着
	)

	out := buf.String()
	for _, secret := range []string{
		"correct-horse-battery", "eyJhbGciOi", testSecret, "pbkdf2-sha256",
	} {
		if strings.Contains(out, secret) {
			t.Errorf("日志里泄露了 %q：%s", secret, out)
		}
	}
	if !strings.Contains(out, "zhangsan") {
		t.Errorf("用户名不是秘密，不该被打码：%s", out)
	}
}

func TestLogLevelIsConfigurable(t *testing.T) {
	var buf bytes.Buffer
	newLogger(&buf, "error", "auth/password-login").Info("这条不该出现")

	if buf.Len() != 0 {
		t.Errorf("LOG_LEVEL=error 时 Info 不该输出：%s", buf.String())
	}
}

// ============================================================
// 参数解析（002 §8.5.1）
// ============================================================

// TestParseArgsRejectsUnknown 挡住"迁移容器变成服务容器"。
//
// 拼错的迁移命令若回落到"那就启动服务吧"，迁移容器就永不退出，
// 主服务永远等不到"迁移完成"——而日志里写着"组件已就绪"。
func TestParseArgsRejectsUnknown(t *testing.T) {
	if _, _, err := parseArgs([]string{"migreate"}); err == nil {
		t.Fatal("拼错的参数必须报错，绝不能回落到启动服务")
	}

	mode, _, err := parseArgs(nil)
	if err != nil || mode != modeServe {
		t.Errorf("不带参数应当启动服务，实际 mode=%q err=%v", mode, err)
	}

	mode, rest, err := parseArgs([]string{"migrate", "down", "2"})
	if err != nil || mode != modeMigrate || len(rest) != 2 {
		t.Errorf("migrate 子命令解析错误：mode=%q rest=%v err=%v", mode, rest, err)
	}
}
