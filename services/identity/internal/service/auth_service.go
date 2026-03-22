package service

import (
	"backend/pkg/auth"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	"github.com/tojinguyen/identity/internal/domain"
	"github.com/tojinguyen/identity/internal/dto"
	"github.com/tojinguyen/identity/internal/repository"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type AuthService interface {
	Register(ctx context.Context, email, password, name string) (*dto.RegisterResponse, error)
	Login(ctx context.Context, email, password string) (*dto.LoginResponse, error)
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
}

type authService struct {
	userRepo      repository.UserRepository
	authenticator *auth.Authenticator
	oauthConfig   *oauth2.Config
}

func NewAuthService(userRepo repository.UserRepository, authenticator *auth.Authenticator, googleClientID, googleSecret, redirectURL string) AuthService {
	conf := &oauth2.Config{
		ClientID:     googleClientID,
		ClientSecret: googleSecret,
		RedirectURL:  redirectURL,
		Scopes: []string{
			"https://www.googleapis.com/auth/userinfo.email",
			"https://www.googleapis.com/auth/userinfo.profile",
		},
		Endpoint: google.Endpoint,
	}

	return &authService{
		userRepo:      userRepo,
		authenticator: authenticator,
		oauthConfig:   conf,
	}
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

func (s *authService) LoginWithGoogle(ctx context.Context, code string) (*dto.LoginResponse, error) {
	token, err := s.oauthConfig.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("google code exchange failed: %w", err)
	}

	googleUser, err := s.fetchGoogleProfile(token.AccessToken)
	if err != nil {
		return nil, err
	}

	user, err := s.userRepo.GetByEmail(ctx, googleUser.Email)
	if err != nil {
		user = &domain.User{
			Email: googleUser.Email,
			Name:  googleUser.Name,
			Role:  "user",
		}
		if err := s.userRepo.Create(ctx, user); err != nil {
			return nil, err
		}
	}

	access_token, err := s.authenticator.GenerateAccessToken(user.Id, user.Role)
	if err != nil {
		return nil, err
	}

	refresh_token, err := s.authenticator.GenerateRefreshToken(user.Id, user.Role)
	if err != nil {
		return nil, err
	}

	return &dto.LoginResponse{
		AccessToken:  access_token,
		RefreshToken: refresh_token,
		User: dto.UserResponse{
			Id:    user.Id,
			Email: user.Email,
			Name:  user.Name,
		},
	}, nil
}

func (s *authService) GetGoogleAuthURL(state string) string {
	return s.oauthConfig.AuthCodeURL(state)
}

func (s *authService) fetchGoogleProfile(accessToken string) (*dto.GoogleUser, error) {
	resp, err := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + accessToken)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var gUser dto.GoogleUser
	if err := json.NewDecoder(resp.Body).Decode(&gUser); err != nil {
		return nil, err
	}
	return &gUser, nil
}
