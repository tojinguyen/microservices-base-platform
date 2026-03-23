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
