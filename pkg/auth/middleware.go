package auth

import (
	"net/http"

	"backend/pkg/errors"
	"backend/pkg/response"
)

func (a *Authenticator) RequireAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			authHeader := r.Header.Get("Authorization")
			tokenString := ExtractToken(authHeader)

			if tokenString == "" {
				response.Error(w, r, errors.Unauthorized("Missing or malformed Authorization header"))
				return
			}

			claims, err := a.VerifyToken(tokenString)
			if err != nil {
				response.Error(w, r, errors.Unauthorized("Invalid or expired token"))
				return
			}

			ctx := WithUser(r.Context(), claims)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
