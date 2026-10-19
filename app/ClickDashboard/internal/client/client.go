// Package client — gRPC-обёртка над StorageGateway.
//
// Особенности:
//   - Все методы принимают context.Context и уважают его deadline.
//   - Поверх ctx ставится per-RPC таймаут из конфига (timeout).
//   - Конвертация protobuf-типов в доменные — здесь, чтобы выше client
//     ничего не знало про pb.*.
package client

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"go.services.communication.dzen/app/ClickDashboard/internal/domain"
	"go.services.communication.dzen/app/ClickDashboard/pkg/pb"
	"go.services.communication.dzen/app/ClickDashboard/pkg/reqid"
)

// StorageGatewayClient — gRPC-клиент к StorageGateway.
type StorageGatewayClient struct {
	conn    *grpc.ClientConn
	cli     pb.StorageGatewayClient
	timeout time.Duration
}

func NewStorageGatewayClient(addr string, timeout time.Duration) (*StorageGatewayClient, error) {
	conn, err := grpc.NewClient(
		addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		// Ответ за диапазон — обычно сотни КБ. 16 МБ — с запасом.
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(16*1024*1024)),
	)
	if err != nil {
		return nil, fmt.Errorf("dial storage gateway: %w", err)
	}
	return &StorageGatewayClient{
		conn:    conn,
		cli:     pb.NewStorageGatewayClient(conn),
		timeout: timeout,
	}, nil
}

// Close закрывает соединение.
func (c *StorageGatewayClient) Close() error { return c.conn.Close() }

// GetAuthors дёргает StorageGateway.GetAuthors.
func (c *StorageGatewayClient) GetAuthors(ctx context.Context) ([]domain.Author, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	ctx = withReqID(ctx)

	resp, err := c.cli.GetAuthors(ctx, &pb.GetAuthorsRequest{})
	if err != nil {
		return nil, fmt.Errorf("get authors: %w", err)
	}
	out := make([]domain.Author, len(resp.Authors))
	for i, a := range resp.Authors {
		out[i] = domain.Author{
			ID:        a.Id,
			FirstName: a.FirstName,
			LastName:  a.LastName,
			Email:     a.Email,
		}
	}
	return out, nil
}

// GetStatsRange Возвращает map[dateStr][]AuthorStats для снапшета
func (c *StorageGatewayClient) GetStatsRange(ctx context.Context, from, to time.Time) (map[string][]domain.AuthorStats, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	ctx = withReqID(ctx)

	req := &pb.GetStatsRangeRequest{
		FromDate: from.Format("2006-01-02"),
		ToDate:   to.Format("2006-01-02"),
	}
	resp, err := c.cli.GetStatsRange(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("get stats range: %w", err)
	}

	out := make(map[string][]domain.AuthorStats, len(resp.Days))
	for _, day := range resp.Days {
		stats := make([]domain.AuthorStats, len(day.Stats))
		for i, s := range day.Stats {
			stats[i] = domain.AuthorStats{AuthorID: s.AuthorId, Count: s.Count}
		}
		out[day.Date] = stats
	}
	return out, nil
}

// withReqID пробрасывает request_id
func withReqID(ctx context.Context) context.Context {
	if id := reqid.FromContext(ctx); id != "" {
		return metadata.AppendToOutgoingContext(ctx, "x-request-id", id)
	}
	return ctx
}
