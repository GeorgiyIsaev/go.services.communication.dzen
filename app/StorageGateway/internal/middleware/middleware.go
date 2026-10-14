// Package middleware содержит HTTP-middleware общего назначения.
package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"go.services.communication.dzen/app/StorageGateway/pkg/reqid"
)

const headerRequestID = "X-Request-ID"

// RequestID присваивает каждому запросу уникальный ID
// (или берёт его из заголовка X-Request-ID, если запрос пришёл от другого сервиса),
// кладёт в context.Context и возвращает в ответном заголовке.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(headerRequestID)
		// Отсекаем мусорные значения от клиента.
		if id == "" || len(id) > 128 {
			id = reqid.New()
		}

		w.Header().Set(headerRequestID, id)

		ctx := reqid.WithContext(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Logging пишет access-log по каждому запросу.
// request_id подхватывается автоматически slog-хендлером из ctx,
// поэтому отдельно передавать его в атрибуты не нужно.
func Logging(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rw := &responseWriter{ResponseWriter: w}

			next.ServeHTTP(rw, r)

			level := slog.LevelInfo
			switch {
			case rw.status >= 500:
				level = slog.LevelError
			case rw.status >= 400:
				level = slog.LevelWarn
			}

			logger.LogAttrs(r.Context(), level, "http request",
				slog.String("method", r.Method),
				slog.String("path", r.URL.Path),
				slog.String("query", r.URL.RawQuery),
				slog.Int("status", rw.status),
				slog.Int("bytes", rw.bytes),
				slog.Duration("duration", time.Since(start)),
				slog.String("remote_addr", r.RemoteAddr),
				slog.String("user_agent", r.UserAgent()),
			)
		})
	}
}

// responseWriter запоминает статус и число записанных байт.
type responseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return // повторный WriteHeader игнорируется, как в net/http
	}
	w.status = code
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}
