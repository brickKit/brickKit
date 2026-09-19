package config

// ExistingSecretRef 识别一个 config 值是不是"引用外部系统已建好的 Secret"这个形状：
// { existingSecret: <名>, key: <Secret 里的 key> }，且只有这两个键、都是非空字符串。
//
// 用在 configSchema 声明了 secret: true 的配置项上（由调用方判断，这里只管认形状）——
// 与 resources[].existingSecret 是同一个模式的另一层：数据库密码这类"资源"密钥用真实的
// 结构体字段（平台自己定好了 password/secret-key 这个 key 名，引用外部 Secret 时约定它
// 也叫这个名字），组件自己的 API 密钥这类"配置"密钥没有对应的资源可以挂，只能用值的形状
// 表达——而且外部同步过来的 Secret 未必用组件作者起的配置项名当 key，所以必须显式写 key。
//
// gopkg.in/yaml.v3 把嵌套映射解到 interface{} 时给的是 map[string]interface{}（已用一个
// 独立小程序核实过），不是 map[interface{}]interface{}，所以这里的类型断言是安全的。
func ExistingSecretRef(v any) (secretName, key string, ok bool) {
	m, isMap := v.(map[string]any)
	if !isMap || len(m) != 2 {
		return "", "", false
	}
	secretName, hasSecret := m["existingSecret"].(string)
	key, hasKey := m["key"].(string)
	if !hasSecret || !hasKey || secretName == "" || key == "" {
		return "", "", false
	}
	return secretName, key, true
}
