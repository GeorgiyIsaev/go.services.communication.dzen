package reqid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
)

// HeaderName — HTTP-заголовок, через который request_id передаётся между сервисами.
const HeaderName = "X-Request-ID"

type ctxKey struct{}

var validID = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,128}$`)

func ValidReqID(id string) bool {
	return validID.MatchString(id)
}

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
