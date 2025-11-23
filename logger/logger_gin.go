package logger

import (
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GinLogger 接收gin框架默认的日志

func GinLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		query := c.Request.URL.RawQuery
		c.Next()
		cost := time.Since(start)
		lg.Info(path,
			zap.Int("status", c.Writer.Status()),
			zap.String("method", c.Request.Method),
			zap.String("path", path),
			zap.String("query", query),
			zap.String("ip", c.ClientIP()),
			zap.String("user-agent", c.Request.UserAgent()),
			zap.String("errors", c.Errors.ByType(gin.ErrorTypePrivate).String()),
			zap.Duration("cost", cost),
		)
	}
}

// GinRecovery recover掉项目可能出现的panic，并使用zap记录相关日志
func GinRecovery(stack bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				if isBrokenPipe(err) {
					logBrokenPipe(c, err)
					c.Error(err.(error))
					c.Abort()
					return
				}
				logRecovery(c, err, stack)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}

// isBrokenPipe 检查是否为连接断开错误
func isBrokenPipe(err interface{}) bool {
	if ne, ok := err.(*net.OpError); ok {
		if se, ok := ne.Err.(*os.SyscallError); ok {
			errMsg := strings.ToLower(se.Error())
			return strings.Contains(errMsg, "broken pipe") ||
				strings.Contains(errMsg, "connection reset by peer")
		}
	}
	return false
}

// logBrokenPipe 记录连接断开错误
func logBrokenPipe(c *gin.Context, err interface{}) {
	httpRequest, _ := httputil.DumpRequest(c.Request, false)
	lg.Error(c.Request.URL.Path,
		zap.Any("error", err),
		zap.String("request", string(httpRequest)),
	)
}

// logRecovery 记录panic恢复日志
func logRecovery(c *gin.Context, err interface{}, stack bool) {
	httpRequest, _ := httputil.DumpRequest(c.Request, false)

	fields := []zap.Field{
		zap.Any("error", err),
		zap.String("request", string(httpRequest)),
	}

	if stack {
		fields = append(fields, zap.String("stack", string(debug.Stack())))
	}

	lg.Error("[Recovery from panic]", fields...)
}
