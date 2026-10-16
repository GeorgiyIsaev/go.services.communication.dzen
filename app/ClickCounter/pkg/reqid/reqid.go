// Package reqid хранит request_id в context.Context.
package reqid

import (
	"context"
	"crypto/rand"
	"encoding/hex"
)

const HeaderName = "X-Request-ID"

type ctxKey struct{}

func WithContext(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

func FromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "unknown"
	}
	return hex.EncodeToString(b[:])
}
