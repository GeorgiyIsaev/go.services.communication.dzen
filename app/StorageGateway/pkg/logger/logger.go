// Package logger создаёт slog.Logger с JSON-выводом в stdout
// и автоматическим добавлением request_id из context.Context.
package logger

import (
	"context"
	"log/slog"
	"os"

	"go.services.communication.dzen/app/StorageGateway/pkg/reqid"
)

// New возвращает логгер: JSONHandler в stdout + обёртка,
// которая вытаскивает request_id из ctx и добавляет его в каждую запись.
func New(level slog.Level) *slog.Logger {
	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	})
	return slog.New(&contextHandler{Handler: base})
}

// contextHandler оборачивает любой slog.Handler и обогащает записи
// атрибутами, которые достаёт из context.Context (сейчас — request_id).
type contextHandler struct {
	slog.Handler
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := reqid.FromContext(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, r)
}

// WithAttrs / WithGroup нужно переопределить, иначе цепочка
// logger.With(...) потеряет нашу обёртку и request_id перестанет добавляться.
func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{Handler: h.Handler.WithGroup(name)}
}
