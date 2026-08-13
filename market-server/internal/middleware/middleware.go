// Package middleware 提供 HTTP 中间件。
//
// 这里只放与业务无关的横切关注点：兜底恢复与访问日志。
// 认证与鉴权不在这里——"谁能做什么"由服务层按被访问的资源判断
// （匿名可以读 public 组件，所以没法在中间件里一刀切）。
package middleware

import (
	"log"
	"net/http"
	"time"
)

// Middleware 是标准的处理器装饰器。
type Middleware func(http.Handler) http.Handler

// logfOrDefault 在没给日志函数时退回标准库。
func logfOrDefault(logf func(string, ...any)) func(string, ...any) {
	if logf == nil {
		return log.Printf
	}
	return logf
}

// Recover 兜住 panic，返回 500，并把堆栈留在服务端日志里。
//
// 单个请求打挂整个市场进程是不可接受的：其他人的安装会一起失败。
func Recover(logf func(string, ...any)) Middleware {
	write := logfOrDefault(logf)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}
				write("panic %s %s: %v", r.Method, r.URL.Path, recovered)

				// 响应可能已经写出去一部分，这时再写头只会多一条警告，
				// 但至少要保证连接不是无声中断的
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusInternalServerError)
				_, _ = w.Write([]byte(`{"success":false,"error":{"code":"INTERNAL","message":"市场内部错误"}}`))
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// statusRecorder 记录实际写出的状态码。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(status int) {
	s.status = status
	s.ResponseWriter.WriteHeader(status)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// AccessLog 记录一行访问日志。
//
// 不记录查询串与请求头：前者可能带组件路径以外的信息，
// 后者带 Authorization（008 §5：凭据不进日志）。
func AccessLog(logf func(string, ...any)) Middleware {
	write := logfOrDefault(logf)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			started := time.Now()
			recorder := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(recorder, r)

			status := recorder.status
			if status == 0 {
				status = http.StatusOK
			}
			write("%s %s %d %s", r.Method, r.URL.Path, status, time.Since(started).Round(time.Millisecond))
		})
	}
}
