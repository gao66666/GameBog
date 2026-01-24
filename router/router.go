package router

import (
	"net/http"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/handler"
	"github.com/gao66666/GoBlog/logger"
	"github.com/gao66666/GoBlog/middleware"
	"github.com/gao66666/GoBlog/service"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// App 结构体用来持有所有的 Handler，方便路由挂载
type App struct {
	UserHandler    *handler.UserHandler
	ArticleHandler *handler.ArticleHandler
	CommentHandler *handler.CommentHandler
}

// SetupApp 负责依赖注入的组装过程
func SetupApp(db *gorm.DB, rdb *redis.Client) *App {
	// 1. Repository 层 (MySQL & Redis)
	userRepo := database.NewUserRepository(db)
	userRedis := database.NewRedisUserRepository(rdb)
	articleRepo := database.NewArticleRepository(db)
	articleRedis := database.NewRedisArticleRepository(rdb)
	commentRepo := database.NewCommentRepository(db)
	commentRedis := database.NewRedisCommentRepository(rdb)

	// 2. Service 层
	userSvc := service.NewUserService(userRepo, userRedis)
	articleSvc := service.NewArticleService(articleRepo, articleRedis)
	commentSvc := service.NewCommentService(commentRepo, commentRedis)

	// 3. Handler 层
	return &App{
		UserHandler:    handler.NewUserHandler(userSvc),
		ArticleHandler: handler.NewArticleHandler(articleSvc),
		CommentHandler: handler.NewCommentHandler(commentSvc),
	}
}

func setGinMode(mode string) {
	switch mode {
	case "dev":
		gin.SetMode(gin.DebugMode)
	case "release":
		gin.SetMode(gin.ReleaseMode)
	default:
		gin.SetMode(gin.TestMode)
	}
}

func RouterInit(mode string, app *App) *gin.Engine {
	// 1. 设置运行模式
	setGinMode(mode)

	//初始化Gin引擎
	r := gin.New()
	// 全局中间件：日志和异常恢复
	r.Use(logger.GinLogger(), logger.GinRecovery(true))
	// --- 公开路由 ---
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })
	r.POST("/signup", app.UserHandler.SignUpHandle)

	r.POST("/login", app.UserHandler.LoginHandle)
	r.GET("/read/:id", app.ArticleHandler.ReadArticleHandle)
	// 获取文章下的楼层列表（分页）
	r.GET("/article/comment/list", app.CommentHandler.GetArticleComment)
	// 获取某一楼的详细回复（楼层详情分页）
	r.GET("/article/comment/floor/:root_id", app.CommentHandler.GetCommentDetail)

	// --- 受保护路由组 ---
	authGroup := r.Group("/v1")
	authGroup.Use(middleware.JWTAuthMiddleware()) // 在这里应用你的 JWT 中间件
	{
		// 示例：只有登录后才能调用的接口
		authGroup.POST("/users/update", app.UserHandler.UpdateUserHandle)
		authGroup.POST("/article/create", app.ArticleHandler.CreateArticleHandle)
		authGroup.GET("/article/list", app.ArticleHandler.GetArticleListHandler)
		authGroup.GET("/article/like", app.ArticleHandler.LikeArticleHandle)
		authGroup.POST("/article/delete/:id", app.ArticleHandler.DeleteArticleHandle)
		authGroup.POST("/article/comment/creat", app.CommentHandler.CreateComment)
	}

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"msg": "404 not found"})
	})

	return r
}
