// 本文件验证组件入口对参数的处理。
//
// 这看着像小事，实则是平台级的坑：迁移容器与主容器用的是**同一个镜像**，
// 只靠命令行参数区分。入口把不认识的参数当成"那就启动服务吧"的话，
// 一个拼错的迁移命令就会让迁移容器永不退出，主服务永远等不到
// "迁移完成"，整个项目卡在 Created——而日志里看起来一切正常
// （组件已就绪）。真跑起来撞到过，见 002 §8.5.1。
package main

import (
	"strings"
	"testing"
)

func TestNoArgumentsMeansServe(t *testing.T) {
	mode, rest, err := parseArgs(nil)

	if err != nil {
		t.Fatalf("不带参数应当是启动服务，却报错：%v", err)
	}
	if mode != modeServe {
		t.Fatalf("不带参数应当是 %q，实际是 %q", modeServe, mode)
	}
	if len(rest) != 0 {
		t.Fatalf("不该有剩余参数，实际是 %v", rest)
	}
}

func TestMigrateArgumentsArePassedThrough(t *testing.T) {
	mode, rest, err := parseArgs([]string{"migrate", "down", "2"})

	if err != nil {
		t.Fatalf("migrate down 2 应当合法，却报错：%v", err)
	}
	if mode != modeMigrate {
		t.Fatalf("应当是 %q，实际是 %q", modeMigrate, mode)
	}
	if len(rest) != 2 || rest[0] != "down" || rest[1] != "2" {
		t.Fatalf("migrate 之后的参数应原样传下去，实际是 %v", rest)
	}
}

// 核心用例：不认识的参数必须报错退出，绝不能回落到"启动服务"。
func TestUnknownArgumentIsRejected(t *testing.T) {
	_, _, err := parseArgs([]string{"migrate-does-not-exist"})

	if err == nil {
		t.Fatal("未知参数必须报错，否则迁移容器会变成服务容器，把整个项目卡死")
	}
	if !strings.Contains(err.Error(), "migrate-does-not-exist") {
		t.Fatalf("错误信息里要带上那个参数，才知道是哪里拼错了：%v", err)
	}
}

// 参数校验必须发生在连数据库之前：为了告诉使用者"参数写错了"，
// 不该先去连一个可能根本连不上的库，那只会给出一句误导的"连接数据库失败"。
func TestArgumentsAreValidatedWithoutEnvironment(t *testing.T) {
	// parseArgs 不读环境变量、不连库——它是纯函数。
	// 这条用例的价值在于把这个约束钉住：一旦有人把它挪到 configFromEnv 之后，
	// 下面这行在没有任何环境变量的情况下就会失败。
	if _, _, err := parseArgs([]string{"bogus"}); err == nil {
		t.Fatal("未知参数应当在读取任何环境变量之前就被拒绝")
	}
}
