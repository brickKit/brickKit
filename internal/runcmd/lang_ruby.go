package runcmd

// probeRuby 刻意收窄，理由同 Python：只认 Rails（bin/rails + Gemfile）。
//
// 用 `ruby bin/rails` 而不是直接执行 bin/rails：binstub 靠 shebang 与可执行位，Windows 上都没有。
// -b 0.0.0.0 是必须的：Rails 开发服务器默认只听回环地址，容器经 host-gateway 够不到它。
func probeRuby(s *scan) outcome {
	if !s.isFile("bin/rails") || !s.isFile("Gemfile") {
		return outcome{}
	}
	return s.ok([]string{"ruby", "bin/rails", "server", "-b", "0.0.0.0", "-p", s.port()}, "bin/rails", "Gemfile")
}
