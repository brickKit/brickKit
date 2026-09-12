package main

import "fmt"

// fmtSscan 单独包一层，只是为了让 main.go 的 import 保持最小。
func fmtSscan(s string, out *int) (int, error) { return fmt.Sscanf(s, "%d", out) }
