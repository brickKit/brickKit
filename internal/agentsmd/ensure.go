package agentsmd

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/docspec"
)

// Mode 是这次调用可以做到哪一步。
type Mode int

const (
	// ModeInit：缺的文件建出来；已有的文件一个字节都不动，只改写已有的维护区。
	ModeInit Mode = iota
	// ModeRepair：使用者明确要求（skills update）——还可以把维护区追加到没有它的 AGENTS.md、
	// 把 @AGENTS.md 追加到没有它的 CLAUDE.md。
	ModeRepair
	// ModeRewrite：只改写已有的维护区（add / remove / upgrade），别的都不碰、也不报。
	ModeRewrite
)

// Result 是这次做了什么，以及没能做、要告诉使用者的问题。
type Result struct {
	AgentsCreated, BlockAppended, BlockRewritten, LegacyReplaced bool
	ClaudeCreated, ClaudeAppended                                bool
	// Problem 非空：AGENTS.md 没有维护区（ErrNoBlock）或标记坏了（ErrMalformed），这次没改它。给人看的说法见 Reason。
	Problem error
	// AgentsMissing：改写模式下 AGENTS.md 根本不在——组件表没人维护了（init / skills update 会建）。
	AgentsMissing bool
	// ClaudeMissingImport：CLAUDE.md 在，但不引 AGENTS.md，这次没改它。
	ClaudeMissingImport bool
}

const filePerm = 0o644

// BlockLang 是 root/AGENTS.md 维护区记的语言；没有文件或没有可用的维护区时 ok 为 false。
func BlockLang(root string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(root, docspec.FileAgents))
	if err != nil {
		return "", false
	}
	b, err := Find(string(data))
	if err != nil {
		return "", false
	}
	return b.Lang, true
}

// Ensure 让 root 下的 AGENTS.md 与 CLAUDE.md 符合 mode 允许的样子。
// isLegacy 判断已有的 AGENTS.md 是不是旧版 CLI 装的那份、没被改过（那时它是技能资产，见 skills.Installer.IsOldAgents）：
// 是的话它本来就是 CLI 的文件，整份换成新骨架。为 nil 时不认旧文件。
func Ensure(root string, c Content, mode Mode, isLegacy func([]byte) bool) (Result, error) {
	var res Result
	path := filepath.Join(root, docspec.FileAgents)
	title := c.Title
	if title == "" {
		title = filepath.Base(root)
	}
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if mode == ModeRewrite {
			res.AgentsMissing = true
			return res, nil
		}
		if err := os.WriteFile(path, []byte(Skeleton(title, c)), filePerm); err != nil {
			return res, err
		}
		res.AgentsCreated = true
	case err != nil:
		return res, err
	default:
		doc := string(data)
		b, findErr := Find(doc)
		switch {
		case findErr == nil:
			if !c.ForceLang {
				c.Lang = b.Lang
			}
			if updated := Replace(doc, b, Render(c)); updated != doc {
				if err := os.WriteFile(path, []byte(updated), filePerm); err != nil {
					return res, err
				}
				res.BlockRewritten = true
			}
		case mode == ModeRewrite:
			res.Problem = findErr // 不改，但要说出来：组件表从此没人维护
			return res, nil
		case errors.Is(findErr, ErrNoBlock) && isLegacy != nil && isLegacy(data):
			if err := os.WriteFile(path, []byte(Skeleton(title, c)), filePerm); err != nil {
				return res, err
			}
			res.LegacyReplaced = true
		case errors.Is(findErr, ErrNoBlock) && mode == ModeRepair:
			if err := os.WriteFile(path, []byte(Append(doc, Render(c))), filePerm); err != nil {
				return res, err
			}
			res.BlockAppended = true
		default:
			res.Problem = findErr
		}
	}
	if mode == ModeRewrite {
		return res, nil
	}
	return res, ensureClaude(root, mode, &res)
}

func ensureClaude(root string, mode Mode, res *Result) error {
	path := filepath.Join(root, docspec.FileClaude)
	data, err := os.ReadFile(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		res.ClaudeCreated = true
		return os.WriteFile(path, []byte(ClaudeContent), filePerm)
	case err != nil:
		return err
	}
	if HasClaudeImport(string(data)) {
		return nil
	}
	if mode != ModeRepair {
		res.ClaudeMissingImport = true
		return nil
	}
	content := string(data)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	res.ClaudeAppended = true
	return os.WriteFile(path, []byte(content+ClaudeContent), filePerm)
}

// HasClaudeImport 判断 CLAUDE.md 的内容里有没有单独一行 @AGENTS.md。
func HasClaudeImport(body string) bool {
	for _, l := range strings.Split(body, "\n") {
		if strings.TrimSpace(l) == docspec.ClaudeImport {
			return true
		}
	}
	return false
}
