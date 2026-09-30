package i18n

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/brickkit/brickkit/internal/msgid"
)

// 查文案的入口只收 msgid.ID：随手写的字符串（一句英文、拼出来的 key）编译都过不了，
// 不用等到运行时显示 !missing-i18n-key!。
func TestLookupsTakeAMessageID(t *testing.T) {
	want := reflect.TypeOf(msgid.ID(""))
	for name, fn := range map[string]any{"T": T, "TN": TN, "Count": Count} {
		assert.Equal(t, want, reflect.TypeOf(fn).In(0), name)
	}
}
