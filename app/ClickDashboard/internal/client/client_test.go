package client

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"go.services.communication.dzen/app/ClickDashboard/pkg/pb"
	"go.services.communication.dzen/app/ClickDashboard/pkg/reqid"
)

// stubServer — in-process реализация StorageGateway для тестов gRPC-клиента.
type stubServer struct {
	pb.UnimplementedStorageGatewayServer

	authors []*pb.Author
	days    []*pb.DailyStats

	authorsErr error
	daysErr    error
	sleep      time.Duration

	// Перехваченные из запроса значения.
	reqIDPresent bool
	gotReqID     string
	gotFrom      string
	gotTo        string
}

func (s *stubServer) GetAuthors(ctx context.Context, _ *pb.GetAuthorsRequest) (*pb.GetAuthorsResponse, error) {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if v := md.Get("x-request-id"); len(v) > 0 {
			s.gotReqID = v[0]
			s.reqIDPresent = true
		}
	}
	if s.sleep > 0 {
		time.Sleep(s.sleep)
	}
	if s.authorsErr != nil {
		return nil, s.authorsErr
	}
	return &pb.GetAuthorsResponse{Authors: s.authors}, nil
}

func (s *stubServer) GetStatsRange(_ context.Context, req *pb.GetStatsRangeRequest) (*pb.GetStatsRangeResponse, error) {
	s.gotFrom, s.gotTo = req.FromDate, req.ToDate
	if s.daysErr != nil {
		return nil, s.daysErr
	}
	return &pb.GetStatsRangeResponse{Days: s.days}, nil
}

// startStub поднимает реальный gRPC-сервер на 127.0.0.1:0 и возвращает адрес.
func startStub(t *testing.T, stub *stubServer) (string, func()) {
	t.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	pb.RegisterStorageGatewayServer(srv, stub)
	go func() { _ = srv.Serve(lis) }()

	return lis.Addr().String(), func() {
		srv.Stop()
	}
}

func TestGetAuthors_Success(t *testing.T) {
	stub := &stubServer{
		authors: []*pb.Author{
			{Id: 1, FirstName: "John", LastName: "Doe", Email: "j@d.com"},
		},
	}
	addr, stop := startStub(t, stub)
	defer stop()

	c, err := NewStorageGatewayClient(addr, 5*time.Second)
	if err != nil {
		t.Fatalf("NewStorageGatewayClient: %v", err)
	}
	defer c.Close()

	authors, err := c.GetAuthors(context.Background())
	if err != nil {
		t.Fatalf("GetAuthors: %v", err)
	}
	if len(authors) != 1 {
		t.Fatalf("len(authors) = %d, want 1", len(authors))
	}
	got := authors[0]
	if got.ID != 1 || got.FirstName != "John" || got.LastName != "Doe" || got.Email != "j@d.com" {
		t.Errorf("author = %+v, want полностью совпадать с pb.Author", got)
	}
}

func TestGetStatsRange_PassesDatesAndConverts(t *testing.T) {
	stub := &stubServer{
		days: []*pb.DailyStats{
			{
				Date: "2024-01-15",
				Stats: []*pb.AuthorStats{
					{AuthorId: 7, Count: 42},
				},
			},
		},
	}
	addr, stop := startStub(t, stub)
	defer stop()

	c, err := NewStorageGatewayClient(addr, 5*time.Second)
	if err != nil {
		t.Fatalf("NewStorageGatewayClient: %v", err)
	}
	defer c.Close()

	from := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC)
	stats, err := c.GetStatsRange(context.Background(), from, to)
	if err != nil {
		t.Fatalf("GetStatsRange: %v", err)
	}

	if stub.gotFrom != "2024-01-01" || stub.gotTo != "2024-01-15" {
		t.Errorf("server got [%q, %q], want [2024-01-01, 2024-01-15]", stub.gotFrom, stub.gotTo)
	}

	day, ok := stats["2024-01-15"]
	if !ok || len(day) != 1 {
		t.Fatalf("stats = %v, want запись за 2024-01-15", stats)
	}
	if day[0].AuthorID != 7 || day[0].Count != 42 {
		t.Errorf("stats[2024-01-15] = %+v, want {7 42}", day[0])
	}
}

func TestGetStatsRange_ServerError(t *testing.T) {
	stub := &stubServer{daysErr: status.Error(codes.Internal, "db down")}
	addr, stop := startStub(t, stub)
	defer stop()

	c, err := NewStorageGatewayClient(addr, 5*time.Second)
	if err != nil {
		t.Fatalf("NewStorageGatewayClient: %v", err)
	}
	defer c.Close()

	_, err = c.GetStatsRange(context.Background(),
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "db down") {
		t.Errorf("error = %v, want содержать причину от сервера", err)
	}
}

func TestRequestID_Propagated(t *testing.T) {
	stub := &stubServer{}
	addr, stop := startStub(t, stub)
	defer stop()

	c, err := NewStorageGatewayClient(addr, 5*time.Second)
	if err != nil {
		t.Fatalf("NewStorageGatewayClient: %v", err)
	}
	defer c.Close()

	ctx := reqid.WithContext(context.Background(), "test-req-id-123")
	if _, err := c.GetAuthors(ctx); err != nil {
		t.Fatalf("GetAuthors: %v", err)
	}
	if !stub.reqIDPresent || stub.gotReqID != "test-req-id-123" {
		t.Errorf("server got req_id=%q (present=%v), want test-req-id-123",
			stub.gotReqID, stub.reqIDPresent)
	}
}

func TestRequestID_NotSetWhenMissing(t *testing.T) {
	stub := &stubServer{}
	addr, stop := startStub(t, stub)
	defer stop()

	c, err := NewStorageGatewayClient(addr, 5*time.Second)
	if err != nil {
		t.Fatalf("NewStorageGatewayClient: %v", err)
	}
	defer c.Close()

	if _, err := c.GetAuthors(context.Background()); err != nil {
		t.Fatalf("GetAuthors: %v", err)
	}
	if stub.reqIDPresent {
		t.Error("x-request-id should not be sent when ctx has no request_id")
	}
}

func TestGetAuthors_ServerError(t *testing.T) {
	stub := &stubServer{authorsErr: status.Error(codes.Internal, "boom")}
	addr, stop := startStub(t, stub)
	defer stop()

	c, err := NewStorageGatewayClient(addr, 5*time.Second)
	if err != nil {
		t.Fatalf("NewStorageGatewayClient: %v", err)
	}
	defer c.Close()

	if _, err := c.GetAuthors(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestGetAuthors_NetworkError(t *testing.T) {
	// Свободный адрес, на котором никто не слушает.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := lis.Addr().String()
	_ = lis.Close()

	c, err := NewStorageGatewayClient(addr, 300*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorageGatewayClient: %v", err)
	}
	defer c.Close()

	if _, err := c.GetAuthors(context.Background()); err == nil {
		t.Fatal("expected error for unreachable server")
	}
}

func TestGetAuthors_Timeout(t *testing.T) {
	stub := &stubServer{sleep: 500 * time.Millisecond}
	addr, stop := startStub(t, stub)
	defer stop()

	c, err := NewStorageGatewayClient(addr, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("NewStorageGatewayClient: %v", err)
	}
	defer c.Close()

	start := time.Now()
	_, err = c.GetAuthors(context.Background())
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Errorf("took %v, want близко к таймауту 50ms", elapsed)
	}
}
