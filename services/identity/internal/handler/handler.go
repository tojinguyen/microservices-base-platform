package handler

import (
	"backend/pkg/response"
	"encoding/json"
	"net/http"

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
