// notify/webhook 是一个最小的 BrickKit 组件：把收到的消息记下来。
//
// 它存在的意义是演示"从零写一个组件要做哪几件事"：
//  1. 配置只从环境变量读
//  2. /healthz 只检查本进程
//  3. 日志是 JSON，带 componentId
//  4. 容器不以 root 运行
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

const listenAddr = ":8080"

type message struct {
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

type store struct {
	mu    sync.Mutex
	items []message
	max   int
}

func (s *store) add(m message) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.items = append(s.items, m)
	if len(s.items) > s.max {
		s.items = s.items[len(s.items)-s.max:]
	}
}

func (s *store) list() []message {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]message, len(s.items))
	copy(out, s.items)
	return out
}

func main() {
	// ① 配置只从环境变量读（002 §1.4）。componentId 由平台注入
	componentID := envOr("COMPONENT_ID", "notify/webhook")
	// configSchema 里声明的 keepMessages → 平台注入 KEEP_MESSAGES
	keep := 20
	if raw := os.Getenv("KEEP_MESSAGES"); raw != "" {
		if n, err := parseInt(raw); err == nil && n > 0 {
			keep = n
		} else {
			fail("KEEP_MESSAGES 必须是正整数，当前是 " + raw)
		}
	}

	// ③ 日志是 JSON，每条都带 componentId（002 §11）
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil)).With("componentId", componentID)

	s := &store{max: keep}
	mux := http.NewServeMux()

	// ② /healthz 只检查本进程存活（002 §9.4）——不查库、不调依赖
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/api/v1/messages", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				Text string `json:"text"`
			}
			if err := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 64<<10)).Decode(&body); err != nil || body.Text == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "需要 text 字段"})
				return
			}
			s.add(message{Text: body.Text, At: time.Now().UTC()})
			logger.Info("收到消息", "length", len(body.Text))
			writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted"})

		case http.MethodGet:
			items := s.list()
			// 空的时候返回 []，不是 null——弱类型的调用方遍历 null 会崩
			if items == nil {
				items = []message{}
			}
			writeJSON(w, http.StatusOK, map[string]any{"messages": items, "total": len(items)})

		default:
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "只支持 GET 与 POST"})
		}
	})

	logger.Info("组件已就绪", "addr", listenAddr, "keepMessages", keep)
	srv := &http.Server{Addr: listenAddr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	if err := srv.ListenAndServe(); err != nil {
		fail(err.Error())
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseInt(s string) (int, error) {
	var n int
	_, err := fmtSscan(s, &n)
	return n, err
}

func fail(msg string) {
	slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("组件启动失败", "error", msg)
	os.Exit(1)
}
