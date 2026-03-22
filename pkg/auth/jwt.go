package auth

import (
	"errors"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type Config struct {
	SecretKey            string `mapstructure:"secret_key"`
	Issuer               string `mapstructure:"issuer"`
	AccessTokenLifespan  int    `mapstructure:"access_token_lifespan"`
	RefreshTokenLifespan int    `mapstructure:"refresh_token_lifespan"`
}

type Authenticator struct {
	config Config
}

func New(cfg Config) *Authenticator {
	return &Authenticator{
		config: cfg,
	}
}

type Claims struct {
	UserID string `json:"user_id"`
	Role   string `json:"role"`
	jwt.RegisteredClaims
}

func (a *Authenticator) GenerateAccessToken(userID, role string) (string, error) {
	claims := &Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    a.config.Issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(a.config.AccessTokenLifespan) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(a.config.SecretKey))
}

func (a *Authenticator) GenerateRefreshToken(userID, role string) (string, error) {
	claims := &Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    a.config.Issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Duration(a.config.RefreshTokenLifespan) * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(a.config.SecretKey))
}

func (a *Authenticator) VerifyToken(tokenString string) (*Claims, error) {
	var parseOptions []jwt.ParserOption
	if a.config.Issuer != "" {
		parseOptions = append(parseOptions, jwt.WithIssuer(a.config.Issuer))
	}

	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(a.config.SecretKey), nil
	}, parseOptions...)

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token claims")
}

func (a *Authenticator) ValidateRefreshToken(tokenString string) (*Claims, error) {
	claims, err := a.VerifyToken(tokenString)
	if err != nil {
		return nil, err
	}
	if time.Until(claims.ExpiresAt.Time) > time.Duration(a.config.AccessTokenLifespan)*time.Hour {
		return nil, errors.New("refresh token is not close to expiration")
	}
	return claims, nil
}

func ExtractToken(authHeader string) string {
	if authHeader == "" {
		return ""
	}
	parts := strings.Split(authHeader, " ")
	if len(parts) == 2 && strings.ToLower(parts[0]) == "bearer" {
		return parts[1]
	}
	return ""
}

func (a *Authenticator) generateToken(userID, role string, lifespan time.Duration) (string, error) {
	claims := &Claims{
		UserID: userID,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    a.config.Issuer,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(lifespan)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(a.config.SecretKey))
}
