package grpc

import (
	"fmt"
	"net"

	"backend/pkg/logger"
	"backend/pkg/userpb"

	"github.com/tojinguyen/identity/internal/repository"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
)

// NewServer creates and registers a gRPC server exposing the UserService.
// Call Serve(lis) on the returned *grpc.Server to start listening.
func NewServer(userRepo repository.UserRepository) *grpc.Server {
	s := grpc.NewServer()
	userpb.RegisterUserServiceServer(s, newUserServiceServer(userRepo))
	reflection.Register(s) // allows grpcurl and other tools to inspect the API
	return s
}

// ListenAndServe starts the gRPC server on the given port. It blocks until
// the server stops; call s.GracefulStop() from a goroutine to shut it down.
func ListenAndServe(s *grpc.Server, port int) error {
	addr := fmt.Sprintf(":%d", port)
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("gRPC listen %s: %w", addr, err)
	}
	logger.L().Info("gRPC server listening", zap.String("addr", addr))
	return s.Serve(lis)
}
