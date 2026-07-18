package auth

type LoginRequest struct {
	Username string `json:"username" binding:"required,max=64"`
	Password string `json:"password" binding:"required,max=72"`
}

type ChangePasswordRequest struct {
	OldPassword string `json:"oldPassword" binding:"required,max=72"`
	NewPassword string `json:"newPassword" binding:"required,min=8,max=72"`
}
