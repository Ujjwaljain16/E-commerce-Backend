package notification

import (
	"context"
	"time"

	accountpb "github.com/Ujjwaljain16/E-commerce-Backend/account/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/notification/pb"
	"github.com/Ujjwaljain16/E-commerce-Backend/pkg/logger"
	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// AccountClient is the part of the account service used to find where to send notifications.
type AccountClient interface {
	GetProfile(ctx context.Context, in *accountpb.GetProfileRequest, opts ...grpc.CallOption) (*accountpb.GetProfileResponse, error)
}

// Service sends notifications for order events and implements the NotificationService gRPC interface.
type Service struct {
	pb.UnimplementedNotificationServiceServer
	repo          Repository
	accounts      AccountClient
	sender        Sender
	log           *logger.Logger
	lookupTimeout time.Duration
}

// NewService creates a notification service.
func NewService(repo Repository, accounts AccountClient, sender Sender, log *logger.Logger) *Service {
	return &Service{repo: repo, accounts: accounts, sender: sender, log: log, lookupTimeout: 3 * time.Second}
}

// ListNotifications returns a user's notifications, newest first.
func (s *Service) ListNotifications(ctx context.Context, req *pb.ListNotificationsRequest) (*pb.ListNotificationsResponse, error) {
	if req.UserId == "" {
		return nil, status.Error(codes.InvalidArgument, "user_id is required")
	}
	if _, err := uuid.Parse(req.UserId); err != nil {
		return nil, status.Error(codes.InvalidArgument, "user_id must be a valid UUID")
	}

	page, pageSize := int(req.Page), int(req.PageSize)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 100 {
		pageSize = 100
	}

	list, total, err := s.repo.ListByUser(ctx, req.UserId, page, pageSize)
	if err != nil {
		s.log.Error(ctx, "Failed to list notifications", map[string]interface{}{"error": err.Error(), "user_id": req.UserId})
		return nil, status.Error(codes.Internal, "failed to list notifications")
	}

	out := make([]*pb.Notification, len(list))
	for i, n := range list {
		out[i] = &pb.Notification{
			Id:        n.ID,
			UserId:    n.UserID,
			EventId:   n.EventID,
			Type:      n.Type,
			Channel:   n.Channel,
			Recipient: n.Recipient,
			Subject:   n.Subject,
			Body:      n.Body,
			Status:    n.Status,
			CreatedAt: timestamppb.New(n.CreatedAt),
		}
	}
	return &pb.ListNotificationsResponse{
		Notifications: out,
		Total:         int32(total),
		Page:          int32(page),
		PageSize:      int32(pageSize),
	}, nil
}
