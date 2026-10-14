// Package reqid хранит request_id в context.Context.
package reqid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

type ctxKey struct{}

// WithContext возвращает контекст с установленным request_id.
func WithContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext извлекает request_id (пустая строка, если его нет).
func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// New генерирует новый request_id: 16 случайных байт в hex (32 символа).
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}
