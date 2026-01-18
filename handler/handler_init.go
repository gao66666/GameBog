package handler

import (
	"github.com/gao66666/GoBlog/service"
)

type UserHandler struct {
	se *service.UserService
}

func NewUserHandler(serve *service.UserService) *UserHandler {
	return &UserHandler{se: serve}
}

type ArticleHandler struct {
	se *service.ArticleService
}

func NewArticleHandler(serve *service.ArticleService) *ArticleHandler {
	return &ArticleHandler{se: serve}
}

type CommentHandler struct {
	se *service.CommentService
}

func NewCommentHandler(serve *service.CommentService) *CommentHandler {
	return &CommentHandler{se: serve}
}


