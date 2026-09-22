package runcmd

import (
	"encoding/json"
	"slices"
	"strings"
)

type packageJSON struct {
	Scripts        map[string]string `json:"scripts"`
	PackageManager string            `json:"packageManager"`
}

// 锁文件与包管理器的对应，顺序即展示顺序。
var nodeLockfiles = []struct{ file, manager string }{
	{"pnpm-lock.yaml", "pnpm"},
	{"yarn.lock", "yarn"},
	{"package-lock.json", "npm"},
	{"npm-shrinkwrap.json", "npm"},
}

// probeNode：有 package.json 且带 start（没有则 dev）脚本 → `<包管理器> run <脚本>`。
//
// start 优先于 dev：start 是生态里唯一有专属简写的入口，也最接近容器镜像里真正跑的那条命令。
// 选错的代价都看得见——start 需要先构建就会立刻崩溃，崩溃现场里就是 node 的报错；
// 想要 dev，写 runCommand 即可。
func probeNode(s *scan) outcome {
	text, present, err := s.read("package.json")
	if !present {
		return outcome{}
	}
	var pkg packageJSON
	if err != nil || json.Unmarshal([]byte(text), &pkg) != nil {
		return problemOutcome(ReasonUnreadableManifest, "package.json")
	}

	script := ""
	for _, name := range []string{"start", "dev"} {
		if strings.TrimSpace(pkg.Scripts[name]) != "" {
			script = name
			break
		}
	}
	if script == "" {
		return problemOutcome(ReasonNoStartScript, "package.json")
	}

	manager, evidence, bad := s.nodeManager(pkg.PackageManager)
	if bad != nil {
		return *bad
	}
	return s.ok([]string{manager, "run", script}, append([]string{"package.json scripts." + script}, evidence...)...)
}

// nodeManager 决定用哪个包管理器：package.json 的 packageManager 字段（corepack 的约定）最权威，
// 其次是锁文件；没有锁文件就用 npm（装 Node 就有的那个）。
func (s *scan) nodeManager(declared string) (manager string, evidence []string, bad *outcome) {
	if declared != "" {
		name, _, _ := strings.Cut(declared, "@")
		switch name {
		case "npm", "pnpm", "yarn":
			return name, []string{"package.json packageManager"}, nil
		}
		o := problemOutcome(ReasonUnsupportedPackageManager, declared)
		return "", nil, &o
	}

	var managers, files []string
	for _, l := range nodeLockfiles {
		if s.isFile(l.file) {
			files = append(files, l.file)
			if !slices.Contains(managers, l.manager) {
				managers = append(managers, l.manager)
			}
		}
	}
	switch len(managers) {
	case 0:
		return "npm", nil, nil
	case 1:
		return managers[0], files, nil
	default:
		o := problemOutcome(ReasonConflictingPackageManagers, "package.json", files...)
		return "", nil, &o
	}
}
