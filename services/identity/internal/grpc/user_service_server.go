package grpc

import (
	"context"

	"backend/pkg/logger"
	"backend/pkg/userpb"

	"github.com/tojinguyen/identity/internal/repository"
	"go.uber.org/zap"
)

const defaultStreamBatchSize = 500

type userServiceServer struct {
	userpb.UnimplementedUserServiceServer
	userRepo repository.UserRepository
}

func newUserServiceServer(userRepo repository.UserRepository) userpb.UserServiceServer {
	return &userServiceServer{userRepo: userRepo}
}

// StreamUsers streams all users matching the filter, starting after req.Cursor (exclusive).
// The server fetches pages of req.BatchSize and streams each record individually.
// The cursor is the last user ID seen; an empty cursor starts from the beginning.
func (s *userServiceServer) StreamUsers(req *userpb.StreamUsersRequest, stream userpb.UserService_StreamUsersServer) error {
	log := logger.L()
	batchSize := int(req.BatchSize)
	if batchSize <= 0 {
		batchSize = defaultStreamBatchSize
	}
	cursor := req.Cursor

	log.Info("gRPC StreamUsers started",
		zap.String("role", req.Role),
		zap.String("cursor", cursor),
		zap.Int("batch_size", batchSize),
	)

	totalStreamed := 0
	for {
		if err := stream.Context().Err(); err != nil {
			return err
		}

		users, err := s.userRepo.ListUsers(stream.Context(), req.Role, cursor, batchSize)
		if err != nil {
			log.Error("ListUsers failed during stream", zap.String("cursor", cursor), zap.Error(err))
			return err
		}
		if len(users) == 0 {
			break
		}

		for _, u := range users {
			if err := stream.Send(&userpb.UserRecord{
				Id:    u.Id.String(),
				Email: u.Email,
				Name:  u.Name,
				Role:  u.Role,
			}); err != nil {
				return err
			}
		}

		cursor = users[len(users)-1].Id.String()
		totalStreamed += len(users)
		if len(users) < batchSize {
			break
		}
	}

	log.Info("gRPC StreamUsers completed", zap.Int("total_streamed", totalStreamed))
	return nil
}

func (s *userServiceServer) CountUsers(ctx context.Context, req *userpb.CountUsersRequest) (*userpb.CountUsersResponse, error) {
	count, err := s.userRepo.CountUsers(ctx, req.Role)
	if err != nil {
		return nil, err
	}
	return &userpb.CountUsersResponse{Count: count}, nil
}
