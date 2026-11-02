package engine

// fileOwner 在 Windows 上没有意义：那里没有 systemd，也就没有 Quadlet。
func fileOwner(string) (string, bool) { return "", false }
