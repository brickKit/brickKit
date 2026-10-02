package deploy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/deploy"
)

// 在宿主机上跑的进程拿到的值里，host.docker.internal 换成 localhost：那是"宿主机"在**容器**里的名字，
// Linux 的宿主机自己解析不了。值是什么形状都要换得到——裸主机名、URL、带账号的 DSN、一段 JSON。
func TestOnHostMachineRewritesTheAlias(t *testing.T) {
	for in, want := range map[string]string{
		"host.docker.internal":                                  "localhost",
		"host.docker.internal:5432":                             "localhost:5432",
		"http://host.docker.internal:8000/v1":                   "http://localhost:8000/v1",
		"postgres://u:p@host.docker.internal/shop":              "postgres://u:p@localhost/shop",
		`[{"config":{"DB_HOST":"host.docker.internal"}}]`:       `[{"config":{"DB_HOST":"localhost"}}]`,
		"nats://host.docker.internal:4222,host.docker.internal": "nats://localhost:4222,localhost",
		"host.docker.internal.":                                 "localhost.",
	} {
		assert.Equal(t, want, deploy.OnHostMachine(in), in)
	}
}

// 其余的值原样保留，包括只是把这个名字含在一个更长的主机名里的：改写它们只会拨到一个不存在的服务上。
func TestOnHostMachineKeepsEverythingElse(t *testing.T) {
	for _, value := range []string{
		"",
		"10.0.1.10",
		"db.internal.example.com",
		"localhost",
		"postgres", // 裸服务名：平台不认它，但也不该替使用者改成别的
		"myhost.docker.internal",
		"db.host.docker.internal:5432",
		"host.docker.internal.example.com",
		"host.docker.internal-backup",
		"host.docker.internals",
	} {
		assert.Equal(t, value, deploy.OnHostMachine(value), value)
	}
}
