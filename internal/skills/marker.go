package skills

// 每份装进项目的技能文件最后一行带一条记录：哪个版本的 CLI 写的、写的时候正文的指纹。
// 记录跟着文件进 Git，同事新克隆下来也知道哪些是 CLI 的、有没有被改过——以前那份
// skills.lock 放在被忽略的 .brickkit/ 里，新克隆里根本没有，技能从此再也升不上去。

import (
	"bytes"
	"regexp"
)

var markerLine = regexp.MustCompile(`(?m)^<!-- brickkit:skill version=(\S+) sum=(sha256:[0-9a-f]+) -->[ \t]*\r?$`)

// normalize 去掉正文末尾的空行与换行：编辑器补上或去掉最后一个换行，不该让文件变成"已手改"。
func normalize(body []byte) []byte { return bytes.TrimRight(body, "\r\n") }

// Mark 返回带记录的内容：正文、一个空行、记录行。
func Mark(body []byte, version string) []byte {
	n := normalize(body)
	out := append(append([]byte(nil), n...), '\n', '\n')
	return append(out, []byte("<!-- brickkit:skill version="+version+" sum="+Sum(n)+" -->\n")...)
}

// ReadMarker 拆出记录：正文（记录行之外的全部内容，记录行之后有人写的也算正文，已 normalize）、
// 版本、记的指纹。没有记录时 ok 为 false，body 是原内容。
func ReadMarker(content []byte) (body []byte, version, recordedSum string, ok bool) {
	locs := markerLine.FindAllSubmatchIndex(content, -1)
	if len(locs) == 0 {
		return content, "", "", false
	}
	m := locs[len(locs)-1]
	body = append(append([]byte(nil), content[:m[0]]...), content[m[1]:]...)
	return normalize(body), string(content[m[2]:m[3]]), string(content[m[4]:m[5]]), true
}
