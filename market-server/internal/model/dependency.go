package model

import (
	"encoding/json"
	"strings"
)

// UnmarshalJSON 解析组件依赖的三种写法：
//
//	"department/tree@1.0.0"                                  标量（component.yaml 的原样转换）
//	{"id": "infra/redis-event-bus@1.0.0", "optional": true}  映射，版本写在 id 里
//	{"id": "department/tree", "version": "1.0.0"}            映射，版本单独一个字段（007 §3.7）
//
// 三种都要认：CLI 发布时原样转换 component.yaml，而 007 的发布示例用的是第三种。
func (d *ComponentDep) UnmarshalJSON(data []byte) error {
	var ref string
	if err := json.Unmarshal(data, &ref); err == nil {
		d.Ref = ref
		d.ID, d.Version = splitRef(ref)
		return nil
	}

	var obj struct {
		ID       string `json:"id"`
		Version  string `json:"version"`
		Optional bool   `json:"optional"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}

	d.Optional = obj.Optional
	d.ID, d.Version = obj.ID, obj.Version
	if obj.Version == "" {
		d.ID, d.Version = splitRef(obj.ID)
	}

	d.Ref = d.ID
	if d.Version != "" {
		d.Ref = d.ID + "@" + d.Version
	}
	return nil
}

// MarshalJSON 统一输出为映射形式，便于市场侧的查询接口消费。
func (d ComponentDep) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		ID       string `json:"id"`
		Version  string `json:"version"`
		Optional bool   `json:"optional,omitempty"`
	}{ID: d.ID, Version: d.Version, Optional: d.Optional})
}

func splitRef(ref string) (id, version string) {
	id, version, _ = strings.Cut(ref, "@")
	return id, version
}
