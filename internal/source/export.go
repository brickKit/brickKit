package source

// 本文件回答 brickkit build 的两个问题（提案 §9.10）：
//
//	IsLocal       这个版本是不是由本地安装源给出的——本地源是正在开发的代码，它的镜像
//	              必须从这份代码构建，不能拿 registry 里的镜像顶替
//	ExportSource  把一个版本的源码树从它的 git tag 导出来——兼容版本、没克隆过的 git 组件
//	              不在任何本地仓库里，也要能构建

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/msgid"
)

// IsLocal 报告本地安装源（或组件自己的 source.type: local）给出的正是这个版本。
func (c *Client) IsLocal(ctx context.Context, id, version string) bool {
	for _, f := range c.fetchersFor(id) {
		if f.kind() != "local" {
			continue
		}
		if raw, err := f.manifestBytes(ctx, id, version); err == nil && manifestMatches(raw, id, version) {
			return true
		}
	}
	return false
}

// sourceExporter 是能导出某个版本源码树的安装源（git 源）。
type sourceExporter interface {
	exportSource(ctx context.Context, componentID, version, dest string) error
}

// ExportSource 把这个版本的源码树写进 dest。只有 git 源做得到；本地源的代码就在盘上
// （调用方直接用那个目录），市场不给源码。
func (c *Client) ExportSource(ctx context.Context, id, version, dest string) error {
	var failures []failure
	for _, f := range c.fetchersFor(id) {
		e, ok := f.(sourceExporter)
		if !ok {
			failures = append(failures, failure{sourceID: f.id(), err: errNotFound})
			continue
		}
		if err := e.exportSource(ctx, id, version, dest); err != nil {
			failures = append(failures, failure{sourceID: f.id(), err: err})
			continue
		}
		return nil
	}
	return c.notFoundError(id+"@"+version, failures, i18n.T(msgid.SourceHintNoSourceToBuild))
}

func (s *gitSource) exportSource(ctx context.Context, componentID, version, dest string) error {
	repoURL, subpath := s.locate(componentID)
	tag := tagPrefix(componentID, subpath) + version
	repo, found, err := s.repoWithTag(ctx, componentID, tag)
	if err != nil {
		return err
	}
	if !found {
		return errNotFound
	}
	tree := tag
	if subpath != "" {
		tree += ":" + subpath
	}
	archive, err := runGit(ctx, repo.dir, "archive", "--format=tar", tree)
	if err != nil {
		return s.failed(componentID, repoURL, err)
	}
	return untar(bytes.NewReader(archive), dest)
}

// untar 把 tar 展开到 dest。条目路径不能走出 dest（绝对路径、..），链接只认指向 dest 里面的。
func untar(r io.Reader, dest string) error {
	root, err := filepath.Abs(dest)
	if err != nil {
		return err
	}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		target := filepath.Join(root, filepath.FromSlash(h.Name))
		if !withinDir(root, target) || throughSymlink(root, target) {
			return errors.New(i18n.T(msgid.SourceArchiveEntryEscapes, h.Name))
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(h.Mode)&0o777|0o600)
			if err != nil {
				return err
			}
			_, copyErr := io.Copy(f, tr)
			closeErr := f.Close()
			if copyErr != nil {
				return copyErr
			}
			if closeErr != nil {
				return closeErr
			}
		case tar.TypeSymlink:
			if filepath.IsAbs(h.Linkname) || !withinDir(root, filepath.Join(filepath.Dir(target), h.Linkname)) {
				continue // 指到外面的链接不建：构建上下文里不该有它
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := os.Symlink(h.Linkname, target); err != nil && !os.IsExist(err) {
				return err
			}
		}
		// 其余类型（git archive 的 pax 全局头等）不是文件，跳过
	}
}

// throughSymlink 报告 target 的上级目录里有没有已经建好的链接：链接之后的路径字面上在 dest 里，
// 实际却可能在外面（x -> .，d/y -> ../x/..，再写 d/y/文件）。经过链接的条目一律拒绝。
func throughSymlink(root, target string) bool {
	rel, err := filepath.Rel(root, filepath.Dir(target))
	if err != nil || rel == "." {
		return false
	}
	p := root
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		p = filepath.Join(p, part)
		if info, err := os.Lstat(p); err == nil && info.Mode()&os.ModeSymlink != 0 {
			return true
		}
	}
	return false
}
