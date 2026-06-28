package grpc

import (
	"context"
	"fmt"
	"io"

	"backend/pkg/userpb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// UserRecord is a local alias so callers don't need to import userpb directly.
type UserRecord = userpb.UserRecord

// IdentityClient is the interface the campaign worker uses to fetch users.
// Using an interface here allows easy mocking in tests.
type IdentityClient interface {
	// CountUsers returns the total number of users matching the optional role filter.
	CountUsers(ctx context.Context, role string) (int64, error)
	// StreamUsers iterates over users matching the filter, starting after cursor (exclusive).
	// Pass cursor="" to start from the beginning. It calls fn for each user; returning an error stops the stream.
	StreamUsers(ctx context.Context, role string, cursor string, batchSize int32, fn func(*UserRecord) error) error
	Close() error
}

type identityClient struct {
	conn   *grpc.ClientConn
	client userpb.UserServiceClient
}

// NewIdentityClient dials the identity gRPC server at addr and returns a client.
// addr format: "host:port" (e.g. "identity-service:50051").
func NewIdentityClient(addr string) (IdentityClient, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial identity gRPC %s: %w", addr, err)
	}
	return &identityClient{
		conn:   conn,
		client: userpb.NewUserServiceClient(conn),
	}, nil
}

func (c *identityClient) CountUsers(ctx context.Context, role string) (int64, error) {
	resp, err := c.client.CountUsers(ctx, &userpb.CountUsersRequest{Role: role})
	if err != nil {
		return 0, err
	}
	return resp.Count, nil
}

func (c *identityClient) StreamUsers(ctx context.Context, role string, cursor string, batchSize int32, fn func(*UserRecord) error) error {
	stream, err := c.client.StreamUsers(ctx, &userpb.StreamUsersRequest{
		Role:      role,
		Cursor:    cursor,
		BatchSize: batchSize,
	})
	if err != nil {
		return fmt.Errorf("open StreamUsers: %w", err)
	}

	for {
		record, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return fmt.Errorf("StreamUsers recv: %w", err)
		}
		if err := fn(record); err != nil {
			return err
		}
	}
}

func (c *identityClient) Close() error {
	return c.conn.Close()
}
