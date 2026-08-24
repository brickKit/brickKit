package deploy_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/deploy"
)

// 从宿主机拨号时，host.docker.internal 必须换成 localhost。
//
// 这条真跑到过：改成"平台不部署基础资源"之后，status 一律拨号，
// 而资源跑在本机时 host 写的是 host.docker.internal——那是 Docker 注入到
// **容器** /etc/hosts 里的名字，Linux 的宿主机自己解析不了。
// 于是四个组件正连着这个库跑得好好的，status 却报：
//
//	不可达（host.docker.internal:15432：no such host）
//
// 对一个完全健康的部署报不可达，比不报还糟——久了就没人看这一栏了。
func TestDialHostRewritesHostMachineAlias(t *testing.T) {
	assert.Equal(t, "localhost", deploy.DialHost(deploy.HostMachineAlias))
}

// 其余地址原样保留：改写它们只会拨到一个不存在的服务上。
func TestDialHostKeepsEverythingElse(t *testing.T) {
	for _, host := range []string{
		"10.0.1.10",
		"db.internal.example.com",
		"localhost",
		"postgres", // 裸服务名：平台不认它，但也不该替使用者改成别的
	} {
		assert.Equal(t, host, deploy.DialHost(host), host)
	}
}
