package compose

import (
	"fmt"
	"strconv"
	"strings"
)

// Manifest 里的资源配额用 K8s 的写法（`100m` / `128Mi`），
// compose 用的是另一套（`0.10` / `128M`）。这里做转换。
//
// 组件作者只需要按 002 写一种写法，两种部署目标各自翻译——
// 否则同一份 Manifest 得为 Docker 和 K8s 各写一遍配额。

// cpuToCompose 把 K8s 的 CPU 写法转成 compose 的 cpus。
//
//	100m → 0.10      1 → 1.00      1500m → 1.50
func cpuToCompose(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	if millis, ok := strings.CutSuffix(value, "m"); ok {
		parsed, err := strconv.ParseFloat(millis, 64)
		if err != nil {
			return "", fmt.Errorf("CPU 配额 %q 不合法（形如 100m 或 1）", value)
		}
		return fmt.Sprintf("%.2f", parsed/1000), nil
	}

	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return "", fmt.Errorf("CPU 配额 %q 不合法（形如 100m 或 1）", value)
	}
	return fmt.Sprintf("%.2f", parsed), nil
}

// memoryToCompose 把 K8s 的内存写法转成 compose 的 memory。
//
//	128Mi → 128M     1Gi → 1G      512M → 512M
//
// compose 的 M / G 与 K8s 的 Mi / Gi 都是 2 的幂，只是写法不同。
func memoryToCompose(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", nil
	}

	for _, suffix := range []struct{ k8s, compose string }{
		{"Ki", "K"}, {"Mi", "M"}, {"Gi", "G"}, {"Ti", "T"},
	} {
		if number, ok := strings.CutSuffix(value, suffix.k8s); ok {
			if _, err := strconv.ParseFloat(number, 64); err != nil {
				return "", fmt.Errorf("内存配额 %q 不合法（形如 128Mi 或 1Gi）", value)
			}
			return number + suffix.compose, nil
		}
	}

	// 已经是 compose 写法（128M / 1G）或纯字节数，原样透传
	return value, nil
}
