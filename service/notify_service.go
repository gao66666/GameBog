package service

import "github.com/gao66666/GoBlog/database"

type NotifyService struct {
	userRepo *database.UserRepository
}

func NewNotifyService(userRp *database.UserRepository) *NotifyService {

	return &NotifyService{
		userRepo: userRp,
	}
}
