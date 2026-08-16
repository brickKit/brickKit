package cli

// 本文件在发布时把镜像 tag 钉成 digest（P29）。
//
// 顺序上它必须排在**签名之前**——理由见 imagedigest.go 的包注释与
// runPublish 里的那段注释。

import (
	"context"
	"encoding/json"

	"github.com/brickkit/brickkit/internal/clierr"
)

// pinImageDigest 把 deployment.image 的 tag 换成 registry 里的 digest。
//
// 三种情况：
//
//	已经是 digest      什么都不做（也不去问 registry）
//	--no-pin-digest   跳过，但**大声警告**——使用者放弃了可复现性
//	其余              解析；失败就阻断发布
func pinImageDigest(ctx context.Context, opts *Options, pkg *publishPackage, f publishFlags) error {
	image := pkg.manifest.Deployment.Image

	if isDigestRef(image) {
		opts.Printf("   ✅ 镜像已钉 digest，跳过解析\n")
		return nil
	}
	if f.noPinDigest {
		opts.Printf("   ⚠️ 跳过 digest 钉住（--no-pin-digest）\n")
		opts.Printf("      签名只锁住了镜像**字符串**：registry 上换掉同名 tag 的话，\n")
		opts.Printf("      签名照样有效，而跑起来的是另一个镜像\n")
		return nil
	}

	resolve := opts.ResolveDigest
	if resolve == nil {
		resolve = defaultDigestResolver()
	}
	digest, err := resolve(ctx, image)
	if err != nil {
		return digestUnresolvable(image, err)
	}

	pinned := repoOf(image) + "@" + digest
	if err := rewriteImage(pkg, pinned); err != nil {
		return err
	}
	opts.Printf("   ✅ 镜像已钉 digest：%s\n", digest)
	return nil
}

// repoOf 去掉 tag，留下仓库部分。
//
// 与 splitImageTag 同一个难点：registry 地址里的端口号也带冒号
// （registry:5000/app），所以只认最后一个路径段里的冒号。
func repoOf(image string) string {
	name, _ := splitImageTag(image)
	return name
}

// rewriteImage 把钉过的镜像写回 Manifest 与待上传的原始文档。
//
// **两处都要改。** document 是 component.yaml 转成的 JSON，签名与上传都基于它；
// 只改 manifest 结构体的话，签名与上传的内容里还是旧 tag。
//
// 走 map 而不是结构体：document 刻意保留了市场认识、而 CLI 还没建模的字段，
// 过一手结构体会把它们丢掉。
func rewriteImage(pkg *publishPackage, pinned string) error {
	var doc map[string]any
	if err := json.Unmarshal(pkg.document, &doc); err != nil {
		return clierr.New(clierr.CodeManifestInvalid, "错误：无法改写 component.yaml 中的镜像地址").
			WithCause(err)
	}

	deployment, ok := doc["deployment"].(map[string]any)
	if !ok {
		return clierr.New(clierr.CodeManifestInvalid,
			"错误：component.yaml 里没有 deployment 段，无法钉住 digest")
	}
	deployment["image"] = pinned

	updated, err := json.Marshal(doc)
	if err != nil {
		return clierr.New(clierr.CodeInternal, "错误：改写镜像地址后无法序列化 Manifest").
			WithCause(err)
	}

	pkg.document = updated
	pkg.manifest.Deployment.Image = pinned
	return nil
}
