// Package msgid 声明 CLI 消息目录的 key。每条消息的常量都在 messages_gen.go 里，
// 由 cmd/gen-msgid 从 internal/i18n/locales/en.yaml 生成：新增一条消息只改目录，
// 再跑 make generate-msgid。常量值本身（不是常量名）就是查表用的 key；
// 什么时候用哪条 key，写在 en.yaml 里那条 key 上方的注释里。
package msgid

// PluralOneSuffix 拼在一个 key 后面，就是它的"单数形式"的 key。key 本身是
// "其他"形式（英文里的复数，也是没有单复数之分的语言——中文——的唯一形式）；
// 只有 i18n.TN / i18n.Count 会用到这个后缀，普通的 i18n.T 永远只查 key 本身。
// 目录里写不写单数形式由语言自己决定：英文写，中文不写。
const PluralOneSuffix = ".one"
