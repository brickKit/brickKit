package compose

import (
	"sort"

	"github.com/brickkit/brickkit/internal/machine"
	"github.com/brickkit/brickkit/internal/manifest"
)

// 以文件交付的配置项（configSchema 的 mount: file）怎样求值、写到哪见 internal/machine 的 secretfiles.go；
// 这里只把那个目录只读挂进容器。

// secretVolume 是把 service 的那个目录只读挂进容器的 volumes 项。
func secretVolume(service string) map[string]any {
	return map[string]any{
		"type":      "bind",
		"source":    "./" + machine.SecretFileDir + "/" + service,
		"target":    manifest.SecretFilesRoot + "/" + service,
		"read_only": true,
	}
}

// applySecretVolumes 给一个 service 挂上它要读的目录：owners 里有文件的那些，按服务名排序。
func (r *renderer) applySecretVolumes(svc map[string]any, owners ...string) {
	sort.Strings(owners)
	var volumes []any
	for _, owner := range owners {
		if r.HasSecretFiles(owner) {
			volumes = append(volumes, secretVolume(owner))
		}
	}
	if len(volumes) > 0 {
		svc["volumes"] = volumes
	}
}

