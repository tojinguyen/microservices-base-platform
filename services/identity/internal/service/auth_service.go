package service

import (
	"backend/pkg/auth"
	"context"
	"errors"

	"github.com/tojinguyen/identity/internal/domain"
	"github.com/tojinguyen/identity/internal/dto"
	"github.com/tojinguyen/identity/internal/repository"
)

type AuthService interface {
	Register(ctx context.Context, email, password, name string) (*dto.RegisterResponse, error)
	Login(ctx context.Context, email, password string) (*dto.LoginResponse, error)
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

type authService struct {
	userRepo      repository.UserRepository
	authenticator *auth.Authenticator
}

func NewAuthService(userRepo repository.UserRepository, authenticator *auth.Authenticator) AuthService {
	return &authService{userRepo: userRepo, authenticator: authenticator}
}

func (s *authService) Register(ctx context.Context, email, password, name string) (*dto.RegisterResponse, error) {
	user := &domain.User{
		Email: email,
		Name:  name,
	}
	if err := user.HashPassword(password); err != nil {
		return nil, err
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}
	return &dto.RegisterResponse{UserId: user.Id}, nil
}

func (s *authService) Login(ctx context.Context, email, password string) (*dto.LoginResponse, error) {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	if !user.CheckPassword(password) {
		return nil, errors.New("invalid credentials")
	}

	accessToken, err := s.authenticator.GenerateAccessToken(user.Id, user.Role)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.authenticator.GenerateRefreshToken(user.Id, user.Role)
	if err != nil {
		return nil, err
	}

	return &dto.LoginResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		User: dto.UserResponse{
			Id:    user.Id,
			Email: user.Email,
			Name:  user.Name,
			Role:  user.Role,
		},
	}, nil
}

func (s *authService) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	return s.userRepo.GetByID(ctx, id)
}
