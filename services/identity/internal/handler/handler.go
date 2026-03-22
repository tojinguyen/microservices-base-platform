package handler

import (
	"backend/pkg/response"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/tojinguyen/identity/internal/dto"
	"github.com/tojinguyen/identity/internal/service"
)

type AuthHandler struct {
	svc service.AuthService
}

func NewAuthHandler(svc service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
		Name     string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, r, err)
		return
	}

	registerResponse, err := h.svc.Register(r.Context(), input.Email, input.Password, input.Name)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.Created(w, registerResponse)
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var input struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		response.Error(w, r, err)
		return
	}

	login_data, err := h.svc.Login(r.Context(), input.Email, input.Password)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, login_data)
}

func (h *AuthHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var refreshTokenReq dto.RefreshTokenRequest
	if err := json.NewDecoder(r.Body).Decode(&refreshTokenReq); err != nil {
		response.Error(w, r, err)
		return
	}

	loginData, err := h.svc.RefreshToken(r.Context(), refreshTokenReq.RefreshToken)
	if err != nil {
		response.Error(w, r, err)
		return
	}
	response.OK(w, loginData)
}

func (h *AuthHandler) GoogleLogin(w http.ResponseWriter, r *http.Request) {
	url := h.svc.GetGoogleAuthURL("random_state_string")
	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func (h *AuthHandler) GoogleCallback(w http.ResponseWriter, r *http.Request) {
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")

	if state != "random_state_string" {
		response.Error(w, r, errors.New("invalid oauth state"))
		return
	}

	tokenData, err := h.svc.LoginWithGoogle(r.Context(), code)
	if err != nil {
		response.Error(w, r, err)
		return
	}

	response.OK(w, tokenData)
}
