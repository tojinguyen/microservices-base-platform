package service

import (
	"backend/pkg/auth"
	"context"
	"errors"

	"github.com/tojinguyen/identity/internal/domain"
	"github.com/tojinguyen/identity/internal/repository"
)

type AuthService interface {
	Register(ctx context.Context, email, password, name string) (string, error)
	Login(ctx context.Context, email, password string) (string, error)
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

type authService struct {
	userRepo      repository.UserRepository
	authenticator *auth.Authenticator
}

func NewAuthService(userRepo repository.UserRepository, authenticator *auth.Authenticator) AuthService {
	return &authService{userRepo: userRepo, authenticator: authenticator}
}

func (s *authService) Register(ctx context.Context, email, password, name string) (string, error) {
	user := &domain.User{
		Email: email,
		Name:  name,
	}
	if err := user.HashPassword(password); err != nil {
		return "", err
	}
	if err := s.userRepo.Create(ctx, user); err != nil {
		return "", err
	}
	return user.Id, nil
}

func (s *authService) Login(ctx context.Context, email, password string) (string, error) {
	user, err := s.userRepo.GetByEmail(ctx, email)
	if err != nil {
		return "", err
	}
	if !user.CheckPassword(password) {
		return "", errors.New("invalid credentials")
	}

	token, err := s.authenticator.GenerateAccessToken(user.Id, user.Role)
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *authService) GetUserByID(ctx context.Context, id string) (*domain.User, error) {
	return s.userRepo.GetByID(ctx, id)
}
