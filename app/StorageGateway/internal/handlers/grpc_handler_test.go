package handlers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.services.communication.dzen/app/StorageGateway/internal/domain"
	"go.services.communication.dzen/app/StorageGateway/pkg/pb"
)

func newTestGRPCHandler(svc StorageService) *GRPCStorageHandler {
	return NewGRPCStorageHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestGRPC_GetAuthors_InternalErrorDoesNotLeak(t *testing.T) {
	svc := &mockSvc{
		getAuthorsFn: func(context.Context) ([]domain.Author, error) {
			return nil, errors.New("pq: relation \"authors\" does not exist")
		},
	}
	h := newTestGRPCHandler(svc)

	_, err := h.GetAuthors(context.Background(), &pb.GetAuthorsRequest{})
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("err is not a status error: %v", err)
	}
	if st.Code() != codes.Internal {
		t.Fatalf("code = %v, want %v", st.Code(), codes.Internal)
	}
	if st.Message() != "internal error" {
		t.Fatalf("message = %q, want \"internal error\"", st.Message())
	}
	if strings.Contains(st.Message(), "relation") || strings.Contains(st.Message(), "pq:") {
		t.Fatalf("raw db error leaked: %q", st.Message())
	}
}

func TestGRPC_GetAuthors_Success(t *testing.T) {
	svc := &mockSvc{
		getAuthorsFn: func(context.Context) ([]domain.Author, error) {
			return []domain.Author{{ID: 1, FirstName: "Ivan", LastName: "Petrov", Email: "i@p.local"}}, nil
		},
	}
	h := newTestGRPCHandler(svc)

	resp, err := h.GetAuthors(context.Background(), &pb.GetAuthorsRequest{})
	if err != nil {
		t.Fatalf("GetAuthors: %v", err)
	}
	if len(resp.Authors) != 1 || resp.Authors[0].Id != 1 {
		t.Fatalf("authors = %+v", resp.Authors)
	}
}

func TestGRPC_GetStatsRange(t *testing.T) {
	tests := []struct {
		name        string
		from, to    string
		svcErr      error
		wantCode    codes.Code
		wantSvcCall bool
	}{
		{
			name:        "success",
			from:        "2025-01-10",
			to:          "2025-01-12",
			wantCode:    codes.OK,
			wantSvcCall: true,
		},
		{
			name:     "invalid from format",
			from:     "10-01-2025",
			to:       "2025-01-12",
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "invalid to format",
			from:     "2025-01-10",
			to:       "yesterday",
			wantCode: codes.InvalidArgument,
		},
		{
			name:     "from after to",
			from:     "2025-01-12",
			to:       "2025-01-10",
			wantCode: codes.InvalidArgument,
		},
		{
			name:        "service internal error",
			from:        "2025-01-10",
			to:          "2025-01-12",
			svcErr:      errors.New("db down: connection refused"),
			wantCode:    codes.Internal,
			wantSvcCall: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svcCalled := false
			svc := &mockSvc{
				getStatsRangeFn: func(_ context.Context, from, to time.Time) (map[string][]domain.AuthorStats, error) {
					svcCalled = true
					if tt.svcErr != nil {
						return nil, tt.svcErr
					}
					return map[string][]domain.AuthorStats{"2025-01-10": {{AuthorID: 1, Count: 5}}}, nil
				},
			}
			h := newTestGRPCHandler(svc)

			resp, err := h.GetStatsRange(context.Background(), &pb.GetStatsRangeRequest{
				FromDate: tt.from,
				ToDate:   tt.to,
			})

			st, _ := status.FromError(err)
			if st.Code() != tt.wantCode {
				t.Fatalf("code = %v, want %v (err=%v)", st.Code(), tt.wantCode, err)
			}
			if svcCalled != tt.wantSvcCall {
				t.Fatalf("svc called = %v, want %v", svcCalled, tt.wantSvcCall)
			}
			if tt.wantCode == codes.OK {
				if len(resp.Days) != 1 {
					t.Fatalf("days = %+v", resp.Days)
				}
			}
			if tt.svcErr != nil && strings.Contains(st.Message(), "db down") {
				t.Fatalf("raw service error leaked: %q", st.Message())
			}
		})
	}
}
