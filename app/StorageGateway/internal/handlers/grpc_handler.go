package handlers

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"go.services.communication.dzen/app/StorageGateway/pkg/pb"
)

type GRPCStorageHandler struct {
	pb.UnimplementedStorageGatewayServer
	svc StorageService
}

func NewGRPCStorageHandler(svc StorageService) *GRPCStorageHandler {
	return &GRPCStorageHandler{svc: svc}
}

func (h *GRPCStorageHandler) GetAuthors(ctx context.Context, req *pb.GetAuthorsRequest) (*pb.GetAuthorsResponse, error) {
	authors, err := h.svc.GetAuthors(ctx)
	if err != nil {
		return nil, err
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

	statsByDate, err := h.svc.GetStatsRange(ctx, from, to)
	if err != nil {
		return nil, err
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
