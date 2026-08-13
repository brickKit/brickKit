package handler

import (
	"errors"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/brickkit/market-server/internal/model"
	"github.com/brickkit/market-server/internal/repo"
)

// maxArtifactSize 是单个产物文件的大小上限。
//
// 产物是契约与文档（proto / openapi / 迁移脚本），不是容器镜像——
// 镜像走镜像仓库（007 §11.4）。给个上限，免得一次误传把磁盘写满。
const maxArtifactSize = 64 << 20 // 64 MiB

// listArtifacts 处理 GET .../artifacts（007 §9.3）。
//
// 响应形状受 CLI 契约（D48）约束：每条必须有 id / type / format / files，
// CLI 靠它把 Manifest 里的产物声明映射到下载用的 artifactId。
func (a *api) listArtifacts(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	records, err := a.svc.ListArtifacts(r.Context(), id, p.componentID(), p["version"])
	if err != nil {
		writeError(w, err)
		return
	}
	if records == nil {
		records = []model.ArtifactRecord{}
	}
	writeJSON(w, http.StatusOK, records)
}

// uploadArtifact 处理 POST .../artifacts/{artifactId}/upload?file=（18.4）。
//
// 请求体就是文件正文。007 §9.3 写的是 POST .../artifacts，这里多了
// artifactId 与 ?file=：一个产物可以声明多个文件（如多份 proto），
// 不指明是哪个文件就没法落到正确的对象键上。一次请求传一个文件，
// 因此也不需要 multipart。
func (a *api) uploadArtifact(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	file := strings.TrimSpace(r.URL.Query().Get("file"))
	if file == "" {
		writeError(w, missingQuery("file", "一个产物可以包含多个文件，必须指明传的是哪一个"))
		return
	}

	// 声明了长度就先拦一道，省得白读一遍再拒绝
	if r.ContentLength > maxArtifactSize {
		writeError(w, tooLargeError())
		return
	}

	body := &limitReader{reader: r.Body, remaining: maxArtifactSize}
	err := a.svc.UploadArtifact(r.Context(), id,
		p.componentID(), p["version"], p["artifactId"], file, body, r.ContentLength)
	if err != nil {
		// 服务层会把读取错误统一包成 INTERNAL，所以"是不是超限"要在这里自己记
		if body.exceeded {
			writeError(w, tooLargeError())
			return
		}
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"componentId": p.componentID(), "version": p["version"],
		"artifactId": p["artifactId"], "file": file,
	})
}

// limitReader 在读满上限后中断，并记下"是超限了"。
//
// 不用 http.MaxBytesReader：它的错误会穿过服务层被统一包成 INTERNAL（500），
// 而文件太大是请求本身的问题，应该是 400。用一个标志位把这件事留在协议层。
type limitReader struct {
	reader    io.Reader
	remaining int64
	exceeded  bool
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.remaining <= 0 {
		l.exceeded = true
		return 0, errTooLarge
	}
	if int64(len(p)) > l.remaining {
		p = p[:l.remaining]
	}
	n, err := l.reader.Read(p)
	l.remaining -= int64(n)
	return n, err
}

// errTooLarge 只在服务端内部流转，对外的说法由 tooLargeError 给。
var errTooLarge = errors.New("产物文件超过大小上限")

func tooLargeError() error {
	return model.Errorf(model.CodeInvalidRequest,
		"产物文件超过大小上限 "+strconv.Itoa(maxArtifactSize/(1<<20))+" MiB").
		WithDetail("limitBytes", maxArtifactSize)
}

// downloadArtifact 处理 GET .../artifacts/{artifactId}/download?file=（18.5、D48）。
//
// 返回的是文件正文本身，不是 JSON 信封：CLI 会把它原样写进
// .brickkit/components/<id>/<版本>/ 下，多一层封装就得多一次解码。
func (a *api) downloadArtifact(w http.ResponseWriter, r *http.Request, p params) {
	id, ok := a.requireIdentity(w, r)
	if !ok {
		return
	}

	file := strings.TrimSpace(r.URL.Query().Get("file"))
	if file == "" {
		writeError(w, missingQuery("file", "一个产物可以包含多个文件，必须指明下载哪一个"))
		return
	}

	reader, err := a.svc.DownloadArtifact(r.Context(), id,
		p.componentID(), p["version"], p["artifactId"], file)
	if err != nil {
		writeError(w, err)
		return
	}
	defer func() { _ = reader.Close() }()

	w.Header().Set("Content-Type", contentTypeOf(file))
	w.Header().Set("Content-Disposition", `attachment; filename="`+path.Base(file)+`"`)
	w.WriteHeader(http.StatusOK)
	// 头已经发出去了，中途出错没法再改状态码，只能中断这次传输
	_, _ = io.Copy(w, reader)
}

// contentTypeOf 按扩展名给出 Content-Type。产物类型有限，穷举即可。
func contentTypeOf(file string) string {
	switch strings.ToLower(path.Ext(file)) {
	case ".json":
		return "application/json; charset=utf-8"
	case ".yaml", ".yml":
		return "application/yaml; charset=utf-8"
	case ".md":
		return "text/markdown; charset=utf-8"
	case ".proto", ".txt", ".sql":
		return "text/plain; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// auditQuery 解析审计查询条件。
func auditQuery(r *http.Request) repo.AuditQuery {
	query := r.URL.Query()
	return repo.AuditQuery{
		ComponentID: strings.TrimSpace(query.Get("componentId")),
		Action:      strings.TrimSpace(query.Get("action")),
		Limit:       positiveInt(query.Get("limit")),
	}
}
