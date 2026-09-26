package envref

import (
	"os"
	"path/filepath"
	"strings"
)

// Lookup 返回 K8s 生成时用的变量查找函数：先看进程环境，再看项目根的 .env。
//
// 顺序不能反：CI 里靠环境变量注入真实密码，本地靠 .env——
// 让 .env 盖掉环境变量，等于让开发机上的假密码顶掉 CI 传进来的真密码。
func Lookup(workDir string) func(string) (string, bool) {
	var dotenv map[string]string

	return func(name string) (string, bool) {
		if value, ok := os.LookupEnv(name); ok {
			return value, true
		}
		if dotenv == nil {
			dotenv = readDotEnv(filepath.Join(workDir, ".env"))
		}
		value, ok := dotenv[name]
		return value, ok
	}
}

// readDotEnv 读项目根目录的 .env。
//
// 文件不存在是正常情况（很多项目不用 .env），返回空表即可。
func readDotEnv(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return map[string]string{}
	}
	return ParseDotEnv(string(data))
}

// parseDotEnv 解析 .env 文件内容，规则贴着 docker compose 自己的解释来——
// 这个文件同时被 `docker compose` 读，两边对同一个文件解释不一致才是真正的
// 问题所在（brickKit 反馈：local-debug.*.env 序列化多行值和特殊字符会截断
// 或解析错误——v0.4.4 复核结果指出，这里从前逐行按 `=` 切、只 Trim 掉值两端
// 各一个引号字符，一个跨多个物理行的双引号/单引号值会在第一行就被切断，
// PEM 私钥这类值只剩第一行）。
//
// 下面每一条规则都是拿真实 `docker compose config` 跑出来对照过的
// （见 up_k8s_test.go），不是凭印象猜的写法：
//
//   - 空行、trim 后以 `#` 开头的整行，当注释跳过
//   - 支持 `export KEY=value` 的 export 前缀
//   - 双引号里的值支持 `\\` `\"` `\n` `\r` `\t` 转义，且可以跨多个物理行——
//     一份跨二十几行的 PEM 私钥，只要整份用双引号包住，就能被当成一个值读全
//   - 单引号里的值原样保留、不处理任何转义，同样可以跨多个物理行
//   - 不加引号的值：等号两侧的空白被去掉；空白紧跟 `#` 才算行内注释
//     （`C=val#hash` 里的 `#` 前面不是空白，不算注释，原样保留）；
//     引号闭合之后同一行剩下的内容原样丢弃（`D="quoted"junk` 取到的是 `quoted`）
func ParseDotEnv(content string) map[string]string {
	out := map[string]string{}

	// 统一成 \n：后面所有下标运算只处理一种换行，回车留给双引号转义
	// （\r）自己产生，不会和"这是不是行尾"这件事混在一起。
	runes := []rune(strings.ReplaceAll(content, "\r\n", "\n"))
	n := len(runes)

	for i := 0; i < n; {
		// 跳过这一行开头的水平空白，判断是不是空行/整行注释
		for i < n && (runes[i] == ' ' || runes[i] == '\t') {
			i++
		}
		if i >= n || runes[i] == '\n' {
			i = skipToNextLine(runes, i)
			continue
		}
		if runes[i] == '#' {
			i = skipToNextLine(runes, i)
			continue
		}

		key, value, ok := parseDotEnvAssignment(runes, &i)
		if ok {
			out[key] = value
		}
		i = skipToNextLine(runes, i)
	}
	return out
}

// parseDotEnvAssignment 解析当前行里的一条 `[export] KEY=value`。
//
// i 进来时指向这一行第一个非空白字符，出去时停在值结束的位置——调用方
// 只管跳到下一行，不关心值到底是被引号闭合、还是不加引号一路读到换行。
func parseDotEnvAssignment(runes []rune, i *int) (key, value string, ok bool) {
	n := len(runes)

	start := *i
	for *i < n && runes[*i] != '\n' && runes[*i] != '=' {
		*i++
	}
	if *i >= n || runes[*i] != '=' {
		return "", "", false // 这一行没有 `=`，不是一条赋值
	}
	key = strings.TrimSpace(string(runes[start:*i]))
	key = strings.TrimSpace(strings.TrimPrefix(key, "export "))
	*i++ // 跳过 `=`

	for *i < n && (runes[*i] == ' ' || runes[*i] == '\t') {
		*i++
	}

	switch {
	case *i < n && runes[*i] == '"':
		value = scanDotEnvQuoted(runes, i, '"', true)
	case *i < n && runes[*i] == '\'':
		value = scanDotEnvQuoted(runes, i, '\'', false)
	default:
		value = scanDotEnvUnquoted(runes, i)
	}
	return key, value, true
}

// scanDotEnvQuoted 从开引号处（*i 指向它）读到匹配的闭引号，返回引号内的内容。
//
// 跨物理行是这个函数存在的全部意义：闭引号找不到就一直往后读，读到的
// 换行符原样计入内容——一份多行 PEM 私钥用一对引号包住，读出来就是
// 包含真换行的完整值，不会在第一个 `\n` 那里假装"这一条赋值结束了"。
func scanDotEnvQuoted(runes []rune, i *int, quote rune, escaped bool) string {
	n := len(runes)
	*i++ // 跳过开引号

	var b strings.Builder
	for *i < n {
		c := runes[*i]
		if c == quote {
			*i++ // 跳过闭引号；闭引号之后同一行剩下的内容留给 skipToNextLine 原样丢弃
			return b.String()
		}
		if escaped && c == '\\' && *i+1 < n {
			switch runes[*i+1] {
			case 'n':
				b.WriteRune('\n')
				*i += 2
				continue
			case 'r':
				b.WriteRune('\r')
				*i += 2
				continue
			case 't':
				b.WriteRune('\t')
				*i += 2
				continue
			case '"':
				b.WriteRune('"')
				*i += 2
				continue
			case '\\':
				b.WriteRune('\\')
				*i += 2
				continue
			}
			// 不认识的转义：反斜杠原样保留，不悄悄吞掉数据
		}
		b.WriteRune(c)
		*i++
	}
	// 一直没找到闭引号：文件写错了，把已经读到的都算数，总比整条丢掉强
	return b.String()
}

// scanDotEnvUnquoted 读一个不加引号的值：读到行尾，" #" 触发行内注释截断，
// 首尾空白去掉。
func scanDotEnvUnquoted(runes []rune, i *int) string {
	n := len(runes)
	start := *i
	end := start
	for *i < n && runes[*i] != '\n' {
		if runes[*i] == '#' && *i > start &&
			(runes[*i-1] == ' ' || runes[*i-1] == '\t') {
			break
		}
		*i++
		end = *i
	}
	return strings.TrimSpace(string(runes[start:end]))
}

// skipToNextLine 把 i 挪到下一行的开头（跳过当前的 `\n`），已经在文件末尾就原地不动。
func skipToNextLine(runes []rune, i int) int {
	n := len(runes)
	for i < n && runes[i] != '\n' {
		i++
	}
	if i < n {
		i++ // 跳过 \n 本身
	}
	return i
}
