package service

import (
	"backend/pkg/auth"
	"backend/pkg/logger"
	"backend/pkg/redis"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/tojinguyen/identity/internal/domain"
	"github.com/tojinguyen/identity/internal/dto"
	"github.com/tojinguyen/identity/internal/repository"
	"go.uber.org/zap"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

type AuthService interface {
	Register(ctx context.Context, email, password, name string) (*dto.RegisterResponse, error)
	Login(ctx context.Context, email, password string) (*dto.LoginResponse, error)
	GetUserByID(ctx context.Context, id string) (*domain.User, error)
	RefreshToken(ctx context.Context, refreshToken string) (*dto.LoginResponse, error)
	LoginWithGoogle(ctx context.Context, code string) (*dto.LoginResponse, error)
	GetGoogleAuthURL(state string) string
	GetProfile(ctx context.Context, userID string) (*dto.UserResponse, error)
	Logout(ctx context.Context, accessToken, refreshToken string) error
}

type authService struct {
	userRepo      repository.UserRepository
	authenticator *auth.Authenticator
	oauthConfig   *oauth2.Config
	cache         *redis.Cache
}

func NewAuthService(userRepo repository.UserRepository, authenticator *auth.Authenticator, cache *redis.Cache, googleClientID, googleSecret, redirectURL string) AuthService {
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
		cache:         cache,
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

	refreshToken, jti, err := s.authenticator.GenerateRefreshToken(user.Id, user.Role)
	if err != nil {
		return nil, err
	}

	rtKey := s.buildRTKey(user.Id.String(), jti)
	expiration := s.getRTExpiration()
	if err := s.cache.Set(ctx, rtKey, "active", expiration); err != nil {
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

func (s *authService) RefreshToken(ctx context.Context, refreshToken string) (*dto.LoginResponse, error) {
	log := logger.FromContext(ctx)

	claims, err := s.authenticator.VerifyToken(refreshToken)
	if err != nil {
		return nil, err
	}

	rtKey := s.buildRTKey(claims.UserID, claims.JTI)
	var status string
	err = s.cache.Get(ctx, rtKey, &status)

	if err != nil {
		log.Warn("Failed to get refresh token status from cache")
		return nil, errors.New("refresh token expired or reused")
	}

	_ = s.cache.Delete(ctx, rtKey)

	user, err := s.userRepo.GetByID(ctx, claims.UserID)
	if err != nil {
		log.Error("Failed to get user by ID", zap.String("user_id", claims.UserID), zap.Error(err))
		return nil, err
	}

	accessToken, err := s.authenticator.GenerateAccessToken(user.Id, user.Role)
	if err != nil {
		log.Error("Failed to generate new access token", zap.Error(err))
		return nil, err
	}

	refreshToken, newJti, err := s.authenticator.GenerateRefreshToken(user.Id, user.Role)
	if err != nil {
		log.Error("Failed to generate new refresh token", zap.Error(err))
		return nil, err
	}

	newRtKey := s.buildRTKey(user.Id.String(), newJti)
	expiration := s.getRTExpiration()
	if err := s.cache.Set(ctx, newRtKey, "active", expiration); err != nil {
		log.Error("Failed to store new refresh token in cache", zap.String("user_id", user.Id.String()), zap.Error(err))
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

	refresh_token, _, err := s.authenticator.GenerateRefreshToken(user.Id, user.Role)
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

func (s *authService) fetchGoogleProfile(accessToken string) (*dto.GoogleUserResponse, error) {
	resp, err := http.Get("https://www.googleapis.com/oauth2/v2/userinfo?access_token=" + accessToken)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var gUser dto.GoogleUserResponse
	if err := json.NewDecoder(resp.Body).Decode(&gUser); err != nil {
		return nil, err
	}
	return &gUser, nil
}

func (s *authService) GetProfile(ctx context.Context, userID string) (*dto.UserResponse, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &dto.UserResponse{
		Id:    user.Id,
		Email: user.Email,
		Name:  user.Name,
		Role:  user.Role,
	}, nil
}

func (s *authService) Logout(ctx context.Context, accessToken, refreshToken string) error {
	atClaims, _ := s.authenticator.VerifyToken(accessToken)
	rtClaims, err := s.authenticator.VerifyToken(refreshToken)

	if err != nil {
		return err
	}

	rtKey := s.buildRTKey(rtClaims.UserID, rtClaims.JTI)
	_ = s.cache.Delete(ctx, rtKey)

	remainingTime := time.Until(atClaims.ExpiresAt.Time)
	if remainingTime > 0 {
		blacklistKey := s.buildBlacklistKey(accessToken)
		_ = s.cache.Set(ctx, blacklistKey, "revoked", remainingTime)
	}

	return s.cache.Delete(ctx, rtKey)
}

func (s *authService) buildRTKey(userID string, jti string) string {
	return fmt.Sprintf("rt:%s:%s", userID, jti)
}

func (s *authService) buildBlacklistKey(accessToken string) string {
	return fmt.Sprintf("blacklist:%s", accessToken)
}

func (s *authService) getRTExpiration() time.Duration {
	return time.Duration(s.authenticator.GetConfig().RefreshTokenLifespan) * time.Hour
}
