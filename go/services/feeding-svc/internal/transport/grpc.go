package transport

import (
	"context"
	"net"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	feedingpb "yunmao.live/proto/feeding/v1"

	"yunmao.live/services/feeding-svc/internal/service"
)

// FeedingGrpcServer 实现 feedingpb.FeedingServiceServer 的 gRPC 端。
// 当前只实现 GetFeedRequest，演示跨服务调用契约；ValidateAndCreate 走 HTTP 接口。
type FeedingGrpcServer struct {
	feedingpb.UnimplementedFeedingServiceServer
	svc *service.FeedingService
}

func NewGrpcServer(svc *service.FeedingService) *FeedingGrpcServer {
	return &FeedingGrpcServer{svc: svc}
}

func (g *FeedingGrpcServer) GetFeedRequest(ctx context.Context, req *feedingpb.GetFeedRequestRequest) (*feedingpb.FeedRequest, error) {
	r, err := g.svc.Get(ctx, req.GetFeedRequestId())
	if err != nil {
		return nil, status.Errorf(codes.NotFound, "%v", err)
	}
	return &feedingpb.FeedRequest{
		FeedRequestId: r.ID,
		UserId:        r.UserID,
		RoomId:        r.RoomID,
		DeviceId:      r.DeviceID,
		AmountGrams:   r.AmountGrams,
		Status:        mapStatus(string(r.Status)),
		CreatedAt:     timestamppb.New(r.CreatedAt),
	}, nil
}

func mapStatus(s string) feedingpb.FeedRequest_Status {
	switch s {
	case "created":
		return feedingpb.FeedRequest_CREATED
	case "accepted":
		return feedingpb.FeedRequest_ACCEPTED
	case "queued":
		return feedingpb.FeedRequest_QUEUED
	case "dispatched":
		return feedingpb.FeedRequest_DISPATCHED
	case "acknowledged":
		return feedingpb.FeedRequest_ACKNOWLEDGED
	case "succeeded":
		return feedingpb.FeedRequest_SUCCEEDED
	case "failed":
		return feedingpb.FeedRequest_FAILED
	case "rejected":
		return feedingpb.FeedRequest_REJECTED
	case "compensated":
		return feedingpb.FeedRequest_COMPENSATED
	}
	return feedingpb.FeedRequest_STATUS_UNSPECIFIED
}

// Serve 启动 gRPC 服务端；调用方在自己的 goroutine 里跑。
func ServeGrpc(addr string, svc *service.FeedingService) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	s := grpc.NewServer()
	feedingpb.RegisterFeedingServiceServer(s, NewGrpcServer(svc))
	return s.Serve(lis)
}
