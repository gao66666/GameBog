package handler

import (
	"github.com/gao66666/GoBlog/models"
	"github.com/gao66666/GoBlog/tool"
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func (h *FollowHandler) CreateFollow(c *gin.Context) {
	p := new(models.ParamFollow)
	if err := c.ShouldBindJSON(p); err != nil {
		zap.L().Info("参数错误")
		tool.ResponseError(c, ErrInvalidParamFollow) // 返回错误响应
		return
	}
	err := h.se.CreateFollow(p)
	if err != nil {
		tool.ResponseError(c, err)
		return
	}
	tool.ResponseSuccess(c, p, "关注成功")
}
