package handler

import (
	"backend/pkg/response"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/tojinguyen/identity/internal/dto"
	"github.com/tojinguyen/identity/internal/service"
)

type AuthHandler struct {
	svc service.AuthService
}

func NewAuthHandler(svc service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

// Register godoc
// @Summary Register a new user
// @Description Register a new user with email and password
// @Tags auth
// @Accept json
// @Produce json
// @Param request body dto.RegisterRequest true "Register Request"
// @Success 201 {object} object
// @Failure 400 {object} object
// @Failure 500 {object} object
// @Router /auth/register [post]
func (h *AuthHandler) Register(c *gin.Context) {
	var input dto.RegisterRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	registerResponse, err := h.svc.Register(c.Request.Context(), input.Email, input.Password, input.Name)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}
	response.Created(c.Writer, registerResponse)
}

// Login godoc
// @Summary Login user
// @Description Login user with email and password
// @Tags auth
// @Accept json
// @Produce json
// @Param request body dto.LoginRequest true "Login Request"
// @Success 200 {object} object
// @Failure 400 {object} object
// @Failure 500 {object} object
// @Router /auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var input dto.LoginRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	login_data, err := h.svc.Login(c.Request.Context(), input.Email, input.Password)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}
	response.OK(c.Writer, login_data)
}

// RefreshToken godoc
// @Summary Refresh token
// @Description Refresh JWT access token
// @Tags auth
// @Accept json
// @Produce json
// @Param request body dto.RefreshTokenRequest true "Refresh Token Request"
// @Success 200 {object} object
// @Failure 400 {object} object
// @Failure 500 {object} object
// @Router /auth/refresh [post]
func (h *AuthHandler) RefreshToken(c *gin.Context) {
	var refreshTokenReq dto.RefreshTokenRequest
	if err := c.ShouldBindJSON(&refreshTokenReq); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	loginData, err := h.svc.RefreshToken(c.Request.Context(), refreshTokenReq.RefreshToken)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}
	response.OK(c.Writer, loginData)
}

func (h *AuthHandler) GoogleLogin(c *gin.Context) {
	url := h.svc.GetGoogleAuthURL("random_state_string")
	c.Redirect(http.StatusTemporaryRedirect, url)
}

func (h *AuthHandler) GoogleCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")

	if state != "random_state_string" {
		response.Error(c.Writer, c.Request, errors.New("invalid oauth state"))
		return
	}

	tokenData, err := h.svc.LoginWithGoogle(c.Request.Context(), code)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}

	response.OK(c.Writer, tokenData)
}

// GetProfile godoc
// @Summary Get user profile
// @Description Get current logged in user profile
// @Tags profile
// @Accept json
// @Produce json
// @Param request body dto.GetProfileRequest true "Get Profile Request"
// @Success 200 {object} object
// @Failure 400 {object} object
// @Failure 401 {object} object
// @Router /profile/ [get]
// @Security ApiKeyAuth
func (h *AuthHandler) GetProfile(c *gin.Context) {
	var input dto.GetProfileRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}
	profileData, err := h.svc.GetProfile(c.Request.Context(), input.UserID)
	if err != nil {
		response.Error(c.Writer, c.Request, err)
		return
	}
	response.OK(c.Writer, profileData)
}
