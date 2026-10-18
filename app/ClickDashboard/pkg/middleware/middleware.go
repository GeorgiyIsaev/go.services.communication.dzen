package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"go.services.communication.dzen/app/ClickDashboard/pkg/reqid"
)

// RequestID присваивает каждому запросу уникальный ID
// (или берёт его из заголовка X-Request-ID, если запрос пришёл от другого сервиса),
// кладёт в context.Context и возвращает в ответном заголовке.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(reqid.HeaderName)
		if !reqid.ValidReqID(id) {
			id = reqid.New()
		}

		w.Header().Set(reqid.HeaderName, id)

		ctx := reqid.WithContext(r.Context(), id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Logging пишет access-log по каждому запросу.
// request_id подхватывается автоматически slog-хендлером из ctx.
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

// Recover ловит панику в хендлерах, логирует её со стеком
// и возвращает 500 вместо обрыва соединения.
func Recover(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					if rec == http.ErrAbortHandler {
						panic(rec)
					}
					logger.ErrorContext(r.Context(), "panic recovered",
						"err", rec,
						"method", r.Method,
						"path", r.URL.Path,
						"stack", string(debug.Stack()),
					)
					writeError(w, http.StatusInternalServerError, "internal error")
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	status      int
	bytes       int
	wroteHeader bool
}

func (w *responseWriter) WriteHeader(code int) {
	if w.wroteHeader {
		return
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

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
