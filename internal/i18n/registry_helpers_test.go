package i18n

import "github.com/brickkit/brickkit/internal/msgid"

// registerForTest 临时登记一种语言并直接给出它的目录（不读 locales/ 下的文件），
// 返回的 restore 撤销登记与目录。只给测试用。
func registerForTest(lang Language, texts map[msgid.ID]string) (restore func()) {
	prevRegistry := registry
	registry = append(append([]Language(nil), registry...), lang)
	loadMu.Lock()
	loaded[lang.Code] = texts
	loadMu.Unlock()
	return func() {
		registry = prevRegistry
		loadMu.Lock()
		delete(loaded, lang.Code)
		loadMu.Unlock()
	}
}
