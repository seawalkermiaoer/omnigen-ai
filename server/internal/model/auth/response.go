package auth

import usermodel "github.com/chenhao/omnigen-ai/server/internal/model/user"

type LoginResponse struct {
	Token string                 `json:"token"`
	User  usermodel.UserResponse `json:"user"`
}
