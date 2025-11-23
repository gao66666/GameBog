package handler

import (
	"github.com/gao66666/GoBlog/service"
)

// handler/user_handler.go
type UserHandler struct {
	se *service.UserService
}

func NewUserHandler(serve *service.UserService) *UserHandler {
	return &UserHandler{se: serve}
}
