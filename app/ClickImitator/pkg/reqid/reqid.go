package reqid

import (
	"context"
)

type ctxKey struct{}

// FromContext извлекает request_id (пустая строка, если его нет).
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}
