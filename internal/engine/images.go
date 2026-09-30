package engine

// 本文件是本机镜像的操作（brickkit build、up 的镜像检查）：只有 docker / podman
// 有"本机镜像"这回事——K8s 从集群能访问的 registry 拉，不经过这台机器，所以它不在 Engine 里。

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
)

// Images 是本机镜像的操作。
type Images interface {
	// ImageExists 报告镜像是否在本机。不在本机不是错误；守护进程连不上才是。
	ImageExists(ctx context.Context, ref string) (bool, error)
	// ImageLabels 返回本机镜像的标签；镜像不在本机时 ok 为 false。
	ImageLabels(ctx context.Context, ref string) (labels map[string]string, ok bool, err error)
	// Build 构建一个镜像。
	Build(ctx context.Context, req BuildRequest) error
}

// BuildRequest 是一次构建。
type BuildRequest struct {
	// Tag 是镜像引用（tag 与组件版本一致）。
	Tag string
	// Context 是构建上下文目录，Dockerfile 是 Dockerfile 的路径（都是绝对路径）。
	Context    string
	Dockerfile string
	// Labels 写进镜像（外壳镜像记下编进去的成员版本）。
	Labels map[string]string
}

// buildOutputLines 是构建失败时保留的输出行数：错误通常在最后，但往上几行才看得出是哪一步。
const buildOutputLines = 20

func (c *Compose) ImageExists(ctx context.Context, ref string) (bool, error) {
	_, found, err := c.inspect(ctx, ref, "{{.Id}}")
	return found, err
}

func (c *Compose) ImageLabels(ctx context.Context, ref string) (map[string]string, bool, error) {
	out, found, err := c.inspect(ctx, ref, "{{json .Config.Labels}}")
	if err != nil || !found {
		return nil, found, err
	}
	labels := map[string]string{}
	if text := strings.TrimSpace(string(out)); text != "" && text != "null" {
		if err := json.Unmarshal([]byte(text), &labels); err != nil {
			return nil, true, err
		}
	}
	return labels, true, nil
}

// inspect 查本机镜像。"没有这个镜像"返回 found=false、err=nil；别的失败（守护进程连不上、
// 没装 docker）照常报错。
func (c *Compose) inspect(ctx context.Context, ref, format string) ([]byte, bool, error) {
	args := []string{"image", "inspect", "--format", format, ref}
	out, err := c.runner(ctx, c.bin, args...)
	if err == nil {
		return out, true, nil
	}
	if !isMissingBinary(err) && containsAny(strings.ToLower(string(out)), "no such image", "no such object", "image not known") {
		return nil, false, nil
	}
	return nil, false, c.failure(args, out, err, 3)
}

func (c *Compose) Build(ctx context.Context, req BuildRequest) error {
	args := []string{"build", "-t", req.Tag, "-f", req.Dockerfile}
	keys := make([]string, 0, len(req.Labels))
	for k := range req.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "--label", k+"="+req.Labels[k])
	}
	args = append(args, req.Context)
	out, err := c.runner(ctx, c.bin, args...)
	if err != nil {
		return c.failure(args, out, err, buildOutputLines)
	}
	return nil
}
