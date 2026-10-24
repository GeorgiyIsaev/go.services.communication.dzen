package handlers

import (
	"context"
	"log/slog"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.services.communication.dzen/app/StorageGateway/pkg/pb"
)

type GRPCStorageHandler struct {
	pb.UnimplementedStorageGatewayServer
	svc    StorageService
	logger *slog.Logger
}

func NewGRPCStorageHandler(svc StorageService, logger *slog.Logger) *GRPCStorageHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &GRPCStorageHandler{svc: svc, logger: logger}
}

func (h *GRPCStorageHandler) GetAuthors(ctx context.Context, req *pb.GetAuthorsRequest) (*pb.GetAuthorsResponse, error) {
	authors, err := h.svc.GetAuthors(ctx)
	if err != nil {
		h.logger.ErrorContext(ctx, "get authors: internal error", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	pbAuthors := make([]*pb.Author, len(authors))
	for i, a := range authors {
		pbAuthors[i] = &pb.Author{
			Id:        a.ID,
			FirstName: a.FirstName,
			LastName:  a.LastName,
			Email:     a.Email,
		}
	}
	return &pb.GetAuthorsResponse{Authors: pbAuthors}, nil
}

func (h *GRPCStorageHandler) GetStatsRange(ctx context.Context, req *pb.GetStatsRangeRequest) (*pb.GetStatsRangeResponse, error) {
	from, err := time.Parse("2006-01-02", req.FromDate)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid from_date format, expected YYYY-MM-DD")
	}
	to, err := time.Parse("2006-01-02", req.ToDate)
	if err != nil {
		return nil, status.Error(codes.InvalidArgument, "invalid to_date format, expected YYYY-MM-DD")
	}
	if from.After(to) {
		return nil, status.Error(codes.InvalidArgument, "from_date must not be after to_date")
	}

	statsByDate, err := h.svc.GetStatsRange(ctx, from, to)
	if err != nil {
		h.logger.ErrorContext(ctx, "get stats range: internal error",
			"err", err, "from", req.FromDate, "to", req.ToDate)
		return nil, status.Error(codes.Internal, "internal error")
	}

	var pbDays []*pb.DailyStats
	for dateStr, stats := range statsByDate {
		pbStats := make([]*pb.AuthorStats, len(stats))
		for i, s := range stats {
			pbStats[i] = &pb.AuthorStats{
				AuthorId: s.AuthorID,
				Count:    s.Count,
			}
		}
		pbDays = append(pbDays, &pb.DailyStats{
			Date:  dateStr,
			Stats: pbStats,
		})
	}

	return &pb.GetStatsRangeResponse{Days: pbDays}, nil
}
