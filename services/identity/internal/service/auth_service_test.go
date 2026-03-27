package service

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/tojinguyen/identity/internal/repository/mocks"
)

func TestAuthService_Register_Success(t *testing.T) {
	ctx := t.Context()

	mockRepo := mocks.NewUserRepository(t)

	svc := &authService{
		userRepo: mockRepo,
	}

	mockRepo.On("Create", mock.Anything, mock.AnythingOfType("*domain.User")).Return(nil)

	resp, err := svc.Register(ctx, "test@gmail.com", "password123", "User A")

	assert.NoError(t, err)
	assert.NotNil(t, resp)
}
