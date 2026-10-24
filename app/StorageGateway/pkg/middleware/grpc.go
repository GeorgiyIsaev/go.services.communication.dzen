package middleware

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"go.services.communication.dzen/app/StorageGateway/pkg/reqid"
)

// GRPCRequestID достает x-request-id из метадаты, кладет в контекст и возвращает в ответ.
func GRPCRequestID() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		md, ok := metadata.FromIncomingContext(ctx)
		var id string
		if ok {
			if vals := md.Get("x-request-id"); len(vals) > 0 {
				id = vals[0]
			}
		}
		if !reqid.ValidReqID(id) {
			id = reqid.New()
		}

		ctx = reqid.WithContext(ctx, id)
		// Отправляем заголовок обратно клиенту
		_ = grpc.SetHeader(ctx, metadata.Pairs("x-request-id", id))

		return handler(ctx, req)
	}
}

// GRPCRecover ловит паники в gRPC хендлерах.
func GRPCRecover(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp interface{}, err error) {
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorContext(ctx, "grpc panic recovered",
					"err", r,
					"method", info.FullMethod,
					"stack", string(debug.Stack()),
				)
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// GRPCLogging пишет логи по каждому gRPC вызову.
func GRPCLogging(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)

		code := status.Code(err).String()
		level := slog.LevelInfo
		if err != nil {
			level = slog.LevelError
		}

		logger.LogAttrs(ctx, level, "grpc request",
			slog.String("method", info.FullMethod),
			slog.String("code", code),
			slog.Duration("duration", time.Since(start)),
		)
		return resp, err
	}
}
