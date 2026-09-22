package runcmd

import "strings"

// probePython 刻意收窄：只认 Django 的 manage.py。Python 没有"这个项目怎么启动"的通用约定
// （Flask、FastAPI 的入口都是用户自己起的名字），猜错了还能跑起来比报错更难排查，
// 所以其余情况一律交给用户手写 runCommand。
//
// runserver 显式绑 0.0.0.0：它默认只听 127.0.0.1，而容器经 host-gateway 访问宿主机上的进程，
// 走的不是回环地址，只听回环的进程对容器不可达。
func probePython(s *scan) outcome {
	text, present, _ := s.read("manage.py")
	if !present || !strings.Contains(strings.ToLower(text), "django") {
		return outcome{}
	}
	python, venv := s.pythonInterpreter()
	return s.ok([]string{python, "manage.py", "runserver", "0.0.0.0:" + s.port()},
		evidenceOf("manage.py", venv)...)
}

// pythonInterpreter 优先用项目自己的虚拟环境（.venv、venv），没有才用 PATH 里的 python。
// 第二个返回值是用到的虚拟环境解释器的相对路径（没用则为空）。
func (s *scan) pythonInterpreter() (string, string) {
	candidates := []string{".venv/bin/python", "venv/bin/python"}
	fallback := "python3"
	if s.windows() {
		candidates = []string{".venv/Scripts/python.exe", "venv/Scripts/python.exe"}
		fallback = "python"
	}
	for _, c := range candidates {
		if s.isFile(c) {
			return s.path(c), c
		}
	}
	return fallback, ""
}

// pythonEnv：子进程的 stdout 是管道而不是终端，Python 会因此整块缓冲，日志要攒够一大块才出来。
// PYTHONUNBUFFERED=1 让它逐行输出（foreman 一类的工具都这么做）。
func pythonEnv(*scan) []string {
	return []string{"PYTHONUNBUFFERED=1"}
}
