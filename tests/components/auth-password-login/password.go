package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"

	"golang.org/x/crypto/pbkdf2"
)

// 口令哈希参数。
//
// 用 PBKDF2-HMAC-SHA256 而不是自己拼 salt+sha256：口令哈希要慢，快的哈希
// 意味着一张显卡一秒能试上百亿次。迭代次数按 OWASP 2023 的建议取 600000。
const (
	pbkdf2Iterations = 600000
	pbkdf2KeyLength  = 32
	saltLength       = 16
	// hashPrefix 标明算法与参数，将来换算法时旧哈希还能认出来该怎么验。
	hashPrefix = "pbkdf2-sha256"
)

// hashPassword 生成带随机盐的口令哈希。
//
// 每次调用的盐都不同，因此同一个口令两次哈希的结果也不同——否则哈希值本身
// 就成了口令的指纹：一眼看出"这两个人用了同一个密码"，彩虹表也能直接查。
func hashPassword(password string) (string, error) {
	if password == "" {
		return "", errors.New("口令不能为空")
	}

	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	key := pbkdf2.Key([]byte(password), salt, pbkdf2Iterations, pbkdf2KeyLength, sha256.New)
	return strings.Join([]string{
		hashPrefix,
		itoa(pbkdf2Iterations),
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	}, "$"), nil
}

// verifyPassword 校验口令。
//
// 哈希本身有问题时（空、格式不对、参数解析不出）一律返回 false。
// 这类"读不懂"的情况绝不能当成通过——库里的一条脏数据不该变成一把万能钥匙。
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != hashPrefix {
		return false
	}

	iterations, ok := atoi(parts[1])
	if !ok || iterations < 1 {
		return false
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[2])
	if err != nil || len(salt) == 0 {
		return false
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil || len(want) == 0 {
		return false
	}

	got := pbkdf2.Key([]byte(password), salt, iterations, len(want), sha256.New)
	// 定时安全比较：普通的 == 会在第一个不同的字节处返回，
	// 攻击者据此可以逐字节把哈希试出来
	return subtle.ConstantTimeCompare(got, want) == 1
}

// atoi 解析一个非负十进制整数。
func atoi(s string) (int, bool) {
	if s == "" {
		return 0, false
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	return n, true
}
