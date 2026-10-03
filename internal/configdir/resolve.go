package configdir

import (
	"fmt"
	"sort"
	"strings"

	"github.com/brickkit/brickkit/internal/clierr"
	"github.com/brickkit/brickkit/internal/i18n"
	"github.com/brickkit/brickkit/internal/manifest"
	"github.com/brickkit/brickkit/internal/msgid"
	"github.com/brickkit/brickkit/internal/yamlcheck"
)

// Origin 说明一个配置值从哪来。
type Origin string

const (
	OriginFile    Origin = "file"
	OriginVar     Origin = "var"
	OriginDefault Origin = "default"
)

// Resolved 是一个配置项的最终值。Key 就是环境变量名；
// Value 已经穿过了 $var:，但 ${VAR} / file:// / existingSecret 仍是引用——何时求值是渲染器的事。
type Resolved struct {
	Key     string
	Value   Value
	Origin  Origin
	VarName string
	Secret  bool
	// Mount 是 configSchema 里这一项的 mount（manifest.MountFile 或空）。
	Mount string
}

// Input 是解析一个组件配置所需的全部输入。
type Input struct {
	ComponentID string
	Version     string
	Schema      *manifest.ConfigSchema
	// File 是该组件（该版本）的配置文件，没有时为 nil。
	File *File
	// Vars 是 config/vars.yaml；DeployVars 是当前部署文件的 vars:，同名时后者优先。
	Vars       map[string]Value
	DeployVars map[string]Value
}

// Result 是解析结果。
type Result struct {
	// Values 按键名排序，只含真的有值的项。
	Values []Resolved
	// Missing 是 required 却没有值的键（阻断启动由调用方决定措辞）。
	Missing []string
	// Warnings 是不阻断的问题：写了 schema 里没有的键、组件没有 configSchema……
	Warnings []*clierr.Error
}

// Get 取一个键的解析结果。
func (r *Result) Get(key string) (Resolved, bool) {
	for _, v := range r.Values {
		if v.Key == key {
			return v, true
		}
	}
	return Resolved{}, false
}

// LookupVar 按 deploy 文件的 vars: → config/vars.yaml 的顺序查公共变量。
func LookupVar(name string, deployVars, vars map[string]Value) (Value, bool) {
	if v, ok := deployVars[name]; ok {
		return v, true
	}
	v, ok := vars[name]
	return v, ok
}

// Resolve 按优先级算出一个组件每个配置项的值：
// 组件配置文件里写了且有值 → schema 默认值 → 没有（必填即缺失）。
func Resolve(in Input) (*Result, error) {
	res := &Result{}
	ref := in.ComponentID + "@" + in.Version
	path := ""
	if in.File != nil {
		path = in.File.Path
	}

	if in.Schema == nil {
		if keys := writtenKeys(in.File); len(keys) > 0 {
			res.Warnings = append(res.Warnings, noSchemaWarning(ref, path, keys))
		}
		return res, nil
	}

	written := in.File.Map()
	required := map[string]bool{}
	for _, key := range in.Schema.Required {
		required[key] = true
	}

	var undefined []string
	for _, key := range sortedProperties(in.Schema.Properties) {
		prop := in.Schema.Properties[key]
		r := Resolved{Key: key, Secret: prop.Secret, Mount: prop.Mount}
		v, isWritten := written[key]

		// 先看使用者给没给值：null 等于没给；必填键上的空串是骨架留的空位，也等于没给；
		// 可选键上的空串是明确的值。$var: 引到的值按同一条规则判断。
		given := false
		switch {
		case isWritten && v.Kind == KindVarRef:
			target, found := LookupVar(v.Name, in.DeployVars, in.Vars)
			if !found {
				undefined = append(undefined, key+" → "+v.String())
				continue
			}
			if counts(target, required[key]) {
				r.Value, r.Origin, r.VarName, given = target, OriginVar, v.Name, true
			}
		case isWritten && counts(v, required[key]):
			r.Value, r.Origin, given = v, OriginFile, true
		}
		if !given {
			if prop.Default == nil {
				if required[key] {
					res.Missing = append(res.Missing, key)
				}
				continue
			}
			r.Value, r.Origin = defaultValue(prop.Default), OriginDefault
		}

		if r.Value.Kind == KindSecretRef && !prop.Secret {
			res.Warnings = append(res.Warnings, secretRefWarning(ref, path, key))
			continue
		}
		res.Values = append(res.Values, r)
	}

	res.Warnings = append(res.Warnings, unknownKeyWarnings(ref, path, in.File, in.Schema)...)
	if len(undefined) > 0 {
		return nil, undefinedVarError(ref, path, undefined)
	}
	return res, nil
}

// counts 判断一个写下的值算不算"给了值"。
func counts(v Value, required bool) bool {
	if v.IsUnset() {
		return false
	}
	return !required || !v.IsEmpty()
}

func defaultValue(d any) Value {
	v, err := Literal(d)
	if err != nil {
		return literal(fmt.Sprint(d))
	}
	return v
}

func sortedProperties(props map[string]manifest.ConfigProperty) []string {
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// writtenKeys 返回文件里真的写了值的键，排序。
func writtenKeys(f *File) []string {
	var out []string
	for key, v := range f.Map() {
		if !v.IsUnset() {
			out = append(out, key)
		}
	}
	sort.Strings(out)
	return out
}

func unknownKeyWarnings(ref, path string, f *File, schema *manifest.ConfigSchema) []*clierr.Error {
	known := sortedProperties(schema.Properties)
	var out []*clierr.Error
	for _, key := range f.Keys() {
		if _, declared := schema.Properties[key]; declared {
			continue
		}
		w := clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.ConfigdirUnknownKey, ref, key)).
			WithDetail(i18n.T(msgid.LabelFile), path)
		if len(known) > 0 {
			w = w.WithDetail(i18n.T(msgid.ConfigdirLabelDeclared), strings.Join(known, i18n.T(msgid.ListSeparator)))
		}
		if guess := yamlcheck.Closest(key, known); guess != "" {
			w = w.WithHint(i18n.T(msgid.ConfigdirUnknownKeyGuess, guess))
		}
		out = append(out, w)
	}
	return out
}

func noSchemaWarning(ref, path string, keys []string) *clierr.Error {
	return clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.ConfigdirNoSchema, ref)).
		WithDetail(i18n.T(msgid.LabelFile), path).
		WithDetail(i18n.T(msgid.ConfigdirLabelIgnoredKeys), strings.Join(keys, i18n.T(msgid.ListSeparator)))
}

func secretRefWarning(ref, path, key string) *clierr.Error {
	return clierr.Warn(clierr.CodeConfigInvalid, i18n.T(msgid.ConfigdirSecretRefNotSecret, ref, key)).
		WithDetail(i18n.T(msgid.LabelFile), path)
}

func undefinedVarError(ref, path string, refs []string) *clierr.Error {
	err := clierr.New(clierr.CodeConfigInvalid, i18n.T(msgid.ConfigdirUndefinedVar, ref)).
		WithDetail(i18n.T(msgid.LabelFile), path)
	for _, r := range refs {
		err = err.WithDetail(i18n.T(msgid.ConfigdirLabelUndefinedRef), r)
	}
	return err.WithHint(i18n.T(msgid.ConfigdirHintDefineVar))
}
