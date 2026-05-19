package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"backend/pkg/auth"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/tojinguyen/identity/internal/domain"
	"github.com/tojinguyen/identity/internal/repository/mocks"
	"backend/pkg/logger"
)

type dummyCache struct{}

func (c *dummyCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error { return nil }
func (c *dummyCache) Get(ctx context.Context, key string, dest interface{}) error { return nil }
func (c *dummyCache) Delete(ctx context.Context, keys ...string) error { return nil }

func setupAuthServiceTest(t *testing.T) (*authService, *mocks.UserRepository, *auth.Authenticator) {
	_ = logger.Init("test-identity", "test")
	mockRepo := mocks.NewUserRepository(t)

	authCfg := auth.Config{
		SecretKey:            "test-secret-key-1234567890123456789012",
		Issuer:               "test-issuer",
		AccessTokenLifespan:  100,
		RefreshTokenLifespan: 24,
	}
	authenticator := auth.New(authCfg)

	svc := &authService{
		userRepo:      mockRepo,
		authenticator: authenticator,
		cache:         &dummyCache{},
	}

	return svc, mockRepo, authenticator
}

func TestAuthService_Login(t *testing.T) {
	svc, mockRepo, _ := setupAuthServiceTest(t)
	ctx := t.Context()

	userID := uuid.New()
	password := "123456"

	tempUser := &domain.User{}
	_ = tempUser.HashPassword(password)
	hashedPassword := tempUser.PasswordHash

	t.Run("Login_Success", func(t *testing.T) {
		email := "success@test.com"
		user := &domain.User{
			BaseModel:    domain.BaseModel{Id: userID},
			Email:        email,
			PasswordHash: hashedPassword,
			Role:         "user",
		}

		mockRepo.On("GetByEmail", ctx, email).Return(user, nil).Once()

		resp, err := svc.Login(ctx, email, password)

		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.Equal(t, email, resp.User.Email)
		assert.NotEmpty(t, resp.AccessToken)
	})

	t.Run("Login_InvalidPassword", func(t *testing.T) {
		email := "wrong-pass@test.com"
		user := &domain.User{
			Email:        email,
			PasswordHash: hashedPassword,
		}

		mockRepo.On("GetByEmail", ctx, email).Return(user, nil).Once()

		resp, err := svc.Login(ctx, email, "wrong-password")

		assert.Error(t, err)
		assert.Nil(t, resp)
		assert.Equal(t, "[401] invalid credentials", err.Error())
	})

	t.Run("Login_UserNotFound", func(t *testing.T) {
		email := "notfound@test.com"
		mockRepo.On("GetByEmail", ctx, email).Return((*domain.User)(nil), errors.New("not found")).Once()

		resp, err := svc.Login(ctx, email, password)

		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// 2. TEST REFRESH TOKEN
func TestAuthService_RefreshToken(t *testing.T) {
	svc, mockRepo, authenticator := setupAuthServiceTest(t)
	ctx := t.Context()
	userID := uuid.New()

	t.Run("Refresh_Success", func(t *testing.T) {
		refreshToken, _, _ := authenticator.GenerateRefreshToken(userID, "user")

		user := &domain.User{
			BaseModel: domain.BaseModel{Id: userID},
			Email:     "test@test.com",
			Role:      "user",
		}

		mockRepo.On("GetByID", ctx, userID.String()).Return(user, nil).Once()

		resp, err := svc.RefreshToken(ctx, refreshToken)

		assert.NoError(t, err)
		assert.NotNil(t, resp)
		if assert.NotNil(t, resp) {
			assert.NotEmpty(t, resp.AccessToken)
			assert.NotEmpty(t, resp.RefreshToken)
		}
	})

	t.Run("Refresh_InvalidToken", func(t *testing.T) {
		resp, err := svc.RefreshToken(ctx, "invalid-token-string")
		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}

// 3. TEST GET USER BY ID
func TestAuthService_GetUserByID(t *testing.T) {
	svc, mockRepo, _ := setupAuthServiceTest(t)
	ctx := t.Context()
	userID := uuid.New().String()

	t.Run("GetByID_Success", func(t *testing.T) {
		mockRepo.On("GetByID", ctx, userID).Return(&domain.User{Email: "test@test.com"}, nil).Once()

		user, err := svc.GetUserByID(ctx, userID)

		assert.NoError(t, err)
		assert.Equal(t, "test@test.com", user.Email)
	})
}

// 4. TEST GET PROFILE
func TestAuthService_GetProfile(t *testing.T) {
	svc, mockRepo, _ := setupAuthServiceTest(t)
	ctx := t.Context()
	userID := uuid.New()

	t.Run("GetProfile_Success", func(t *testing.T) {
		user := &domain.User{
			BaseModel: domain.BaseModel{Id: userID},
			Email:     "profile@test.com",
			Name:      "Test Name",
		}
		mockRepo.On("GetByID", ctx, userID.String()).Return(user, nil).Once()

		resp, err := svc.GetProfile(ctx, userID.String())

		assert.NoError(t, err)
		assert.Equal(t, "profile@test.com", resp.Email)
		assert.Equal(t, "Test Name", resp.Name)
	})

	t.Run("GetProfile_NotFound", func(t *testing.T) {
		mockRepo.On("GetByID", ctx, "unknown").Return(nil, errors.New("not found")).Once()

		resp, err := svc.GetProfile(ctx, "unknown")

		assert.Error(t, err)
		assert.Nil(t, resp)
	})
}
