package configdir

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcomment"
)

// HeaderPrefix 是骨架第一行的固定写法（不翻译）：归档恢复靠它认出旧文件属于哪个版本。
const HeaderPrefix = "# Component: "

// Skeleton 按 configSchema 生成组件配置文件骨架。
//
// 只有"必填且没有默认值"的键写成 KEY: ""；其余一律注释掉——没被使用者碰过的键
// 就一直跟随组件的默认值，升级时默认值变了也不会制造假冲突。
func Skeleton(id, version string, schema *manifest.ConfigSchema, varRefs map[string]string) []byte {
	return renderSkeleton(id, version, schema, func(key string, prop manifest.ConfigProperty, required bool) string {
		return skeletonLine(key, prop, varRefs[key], required)
	})
}

// PendingRequired 是骨架写出来之后还空着、要使用者自己填的键：必填、没有默认值、
// 也没有写成 $var: 引用的那些（排好序）。空着它们 up 会拒绝启动，所以 add 要点名。
func PendingRequired(schema *manifest.ConfigSchema, varRefs map[string]string) []string {
	if schema == nil {
		return nil
	}
	requiredKeys, _ := splitKeys(schema)
	var pending []string
	for _, key := range requiredKeys {
		if varRefs[key] == "" {
			pending = append(pending, key)
		}
	}
	return pending
}

// renderSkeleton 是骨架的版式（文件头、必填区、可选区）；每个键写成什么由 line 决定——
// 生成骨架写骨架行，迁移（Migrate）把使用者写过的键换成他的原文或冲突块。
func renderSkeleton(id, version string, schema *manifest.ConfigSchema, line func(key string, prop manifest.ConfigProperty, required bool) string) []byte {
	return renderSkeletonWith(id, version, schema, "", line)
}

// renderSkeletonWith 同 renderSkeleton，另把 preamble（迁移时旧文件开头使用者写的注释）
// 放在生成的文件头之后。
func renderSkeletonWith(id, version string, schema *manifest.ConfigSchema, preamble string, line func(key string, prop manifest.ConfigProperty, required bool) string) []byte {
	if schema == nil || len(schema.Properties) == 0 {
		return nil
	}
	requiredKeys, optionalKeys := splitKeys(schema)

	var b strings.Builder
	b.WriteString(HeaderPrefix + id + "@" + version + "\n")
	b.WriteString(yamlcomment.Block("", i18n.T(msgid.ConfigdirSkeletonIntro)))
	if preamble != "" {
		b.WriteString("\n" + preamble)
	}
	section := func(title string, keys []string, isRequired bool) {
		if len(keys) == 0 {
			return
		}
		b.WriteString("\n" + yamlcomment.Section("", title))
		for _, key := range keys {
			b.WriteString(line(key, schema.Properties[key], isRequired))
		}
	}
	section(i18n.T(msgid.ConfigdirSkeletonRequired), requiredKeys, true)
	section(i18n.T(msgid.ConfigdirSkeletonOptional), optionalKeys, false)
	return []byte(b.String())
}

// splitKeys 把键分成"必填且没有默认值"与其余两组（各自排好序）。
func splitKeys(schema *manifest.ConfigSchema) (requiredKeys, optionalKeys []string) {
	required := map[string]bool{}
	for _, key := range schema.Required {
		required[key] = true
	}
	for _, key := range sortedProperties(schema.Properties) {
		if required[key] && schema.Properties[key].Default == nil {
			requiredKeys = append(requiredKeys, key)
		} else {
			optionalKeys = append(optionalKeys, key)
		}
	}
	return requiredKeys, optionalKeys
}

func skeletonLine(key string, prop manifest.ConfigProperty, varRef string, required bool) string {
	desc := describe(prop)
	switch {
	case varRef != "":
		return fmt.Sprintf("%s: %s%s  # %s\n", key, VarRefPrefix, varRef, desc)
	case required:
		return fmt.Sprintf("%s: \"\"  # %s\n", key, desc)
	case prop.Default != nil:
		return fmt.Sprintf("# %s: %s  # %s (%s)\n", key, defaultText(prop.Default), desc, i18n.T(msgid.ConfigdirSkeletonDefault))
	default:
		return fmt.Sprintf("# %s:  # %s\n", key, desc)
	}
}

// describe 是行尾说明：类型 | secret | 描述（多行描述压成一行）。
func describe(prop manifest.ConfigProperty) string {
	desc := prop.Type
	if prop.Secret {
		desc += " | secret"
	}
	if text := strings.Join(strings.Fields(prop.Description), " "); text != "" {
		desc += " | " + text
	}
	return desc
}

// defaultText 把默认值写成一行：标量走 YAML，列表 / 映射写 JSON（取消注释后仍是合法 YAML）。
func defaultText(v any) string {
	switch v.(type) {
	case string, bool, int, int64, uint64, float64, manifest.Number:
		return ScalarYAML(v)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(data)
	}
}

// Header 从配置文件里读出 "# Component: <id>@<version>" 这一行。
func Header(data []byte) (id, version string, ok bool) {
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, HeaderPrefix) {
			continue
		}
		ref := strings.TrimSpace(strings.TrimPrefix(line, HeaderPrefix))
		id, version, found := manifest.SplitRef(ref)
		if !found || id == "" {
			return "", "", false
		}
		return id, version, true
	}
	return "", "", false
}
