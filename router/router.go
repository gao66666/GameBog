package router

import (
	"net/http"

	"github.com/gao66666/GoBlog/database"
	"github.com/gao66666/GoBlog/handler"
	"github.com/gao66666/GoBlog/logger"
	"github.com/gao66666/GoBlog/middleware"
	"github.com/gao66666/GoBlog/mq"
	"github.com/gao66666/GoBlog/service"
	"github.com/gao66666/GoBlog/setting"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// App 结构体用来持有所有的 Handler，方便路由挂载
type App struct {
	UserHandler              *handler.UserHandler
	ArticleHandler           *handler.ArticleHandler
	CommentHandler           *handler.CommentHandler
	FollowHandler            *handler.FollowHandler
	NotificationHandler      *handler.NotificationHandler
	NotificationStoreHandler *handler.NotificationStoreHandler
	SearchHandler            *handler.SearchHandler
	DMHandler                *handler.DMHandler
	TopicHandler             *handler.TopicHandler
}

func (a *App) StartWorkers() {
	if a.NotificationStoreHandler != nil {
		a.NotificationStoreHandler.StartFlushTicks()
	}
	if err := mq.StartKafkaConsumers(a.NotificationHandler, a.NotificationStoreHandler, a.SearchHandler); err != nil {
		zap.L().Warn("Kafka workers not started", zap.Error(err))
		return
	}
	zap.L().Info("Kafka Worker 启动成功，正在监听【通知】【通知落库】与【搜索同步】Topic...")
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
	notificationRepo := database.NewNotificationRepository(db)
	notificationRedis := database.NewRedisNotificationRepository(rdb)
	followRepo := database.NewFollowRepository(db)
	followRedis := database.NewRedisFollowRepository(rdb)
	dmRepo := database.NewDMRepository(db)
	dmRedis := database.NewRedisDMRepository(rdb)
	topicRepo := database.NewTopicRepository(db)

	if err := userRepo.InitTable(); err != nil {
		zap.L().Warn("用户表初始化失败", zap.Error(err))
	}
	if err := articleRepo.InitTable(); err != nil {
		zap.L().Warn("文章表初始化失败", zap.Error(err))
	}
	if err := commentRepo.InitTable(); err != nil {
		zap.L().Warn("评论表初始化失败", zap.Error(err))
	}
	if err := notificationRepo.InitTable(); err != nil {
		zap.L().Warn("通知表初始化失败", zap.Error(err))
	}
	if err := followRepo.InitTable(); err != nil {
		zap.L().Warn("关注表初始化失败", zap.Error(err))
	}
	if err := dmRepo.InitTable(); err != nil {
		zap.L().Warn("私信表初始化失败", zap.Error(err))
	}
	if err := topicRepo.InitTable(); err != nil {
		zap.L().Warn("话题表初始化失败", zap.Error(err))
	}

	// 2. Service 层
	userSvc := service.NewUserService(userRepo, userRedis)
	articleSvc := service.NewArticleService(articleRepo, userRepo, commentRepo, followRepo, articleRedis, topicRepo)
	followSvc := service.NewFollowService(followRepo, userRepo, followRedis)
	dmSvc := service.NewDMService(dmRepo, userRepo, dmRedis)
	topicSvc := service.NewTopicService(topicRepo, articleRepo)

	// 3. Handler 层
	notificationHandler := handler.NewNotificationHandler(notificationRepo, notificationRedis)
	notificationStoreHandler := handler.NewNotificationStoreHandler(notificationRepo)
	dmHandler := handler.NewDMHandler(dmSvc, notificationHandler)

	commentSvc := service.NewCommentService(commentRepo, articleRepo, userRepo, commentRedis, notificationRepo, notificationRedis, notificationHandler)
	topicHandler := handler.NewTopicHandler(topicSvc)
	return &App{
		UserHandler:              handler.NewUserHandler(userSvc),
		ArticleHandler:           handler.NewArticleHandler(articleSvc),
		CommentHandler:           handler.NewCommentHandler(commentSvc),
		FollowHandler:            handler.NewFollowHandler(followSvc),
		NotificationHandler:      notificationHandler,
		NotificationStoreHandler: notificationStoreHandler,
		SearchHandler:            handler.NewSearchHandler(articleRepo, userRepo, commentRepo),
		DMHandler:                dmHandler,
		TopicHandler:             topicHandler,
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
	// 页面模板与静态资源（最小前端）
	r.LoadHTMLGlob("web/templates/*.tmpl")
	r.Static("/static", "web/static")

	// 全局中间件：日志和异常恢复
	r.Use(
		middleware.RequestID(),
		logger.GinLogger(),
		logger.GinRecovery(true),
		middleware.SecurityHeaders(),
		middleware.RateLimit(setting.Conf.SecurityConfig.RateLimitPerMinute, setting.Conf.SecurityConfig.RateLimitBurst),
	)
	if setting.Conf.ObservabilityConfig.EnableMetrics {
		r.Use(middleware.Metrics())
		r.GET("/metrics", middleware.MetricsHandler)
	}

	// --- 页面路由 ---
	r.GET("/", handler.HomePage)
	r.GET("/login", handler.LoginPage)
	r.GET("/me", handler.MePage)
	r.GET("/u/:id", handler.UserPage)
	r.GET("/dm", handler.DMPage)
	r.GET("/article/:id", handler.ArticlePage)
	r.GET("/editor", handler.EditorPage)
	r.GET("/topics", handler.TopicsPage)
	r.GET("/topic/:id/discuss", handler.TopicDiscussPage)
	r.GET("/topic/:id", handler.TopicPage)

	r.GET("/healthz", handler.Healthz)
	r.GET("/readyz", handler.Readyz)

	// --- 公开路由 ---
	r.GET("/ping", func(c *gin.Context) { c.String(http.StatusOK, "pong") })

	v1 := r.Group("/api/v1")
	registerPublicRoutes(v1, app)
	registerProtectedRoutes(v1, app)

	// 保留旧前缀，减少升级冲击
	legacyV1 := r.Group("/v1")
	registerProtectedRoutes(legacyV1, app)

	r.NoRoute(func(c *gin.Context) {
		c.JSON(http.StatusNotFound, gin.H{"msg": "404 not found"})
	})

	return r
}

func registerPublicRoutes(g *gin.RouterGroup, app *App) {
	g.POST("/signup", app.UserHandler.SignUpHandle)
	g.POST("/login", app.UserHandler.LoginHandle)
	g.GET("/users/:id", app.UserHandler.GetUserPublic)
	g.GET("/users/:id/online", app.NotificationHandler.GetUserOnlineStatus)
	g.GET("/articles/latest", app.ArticleHandler.GetLatestArticlesPublic)
	g.GET("/articles/leaderboard", app.ArticleHandler.GetArticleLeaderboardPublic)
	g.GET("/articles/list", app.ArticleHandler.GetArticleListPublic)
	g.GET("/articles/:id", app.ArticleHandler.ReadArticleHandle)
	g.GET("/articles/comments", app.CommentHandler.GetArticleComment)
	g.GET("/articles/comments/floor/:root_id", app.CommentHandler.GetCommentDetail)
	g.GET("/search", app.SearchHandler.GlobalSearch)
	g.GET("/ws", app.NotificationHandler.HandleWS)

	g.GET("/topics", app.TopicHandler.GetTopicsPublic)
	g.GET("/topics/:id", app.TopicHandler.GetTopicPublic)
	g.GET("/topics/:id/articles", app.TopicHandler.GetTopicArticlesPublic)
	g.GET("/topics/:id/discussions", app.TopicHandler.GetTopicDiscussionsPublic)
}

func registerProtectedRoutes(g *gin.RouterGroup, app *App) {
	authGroup := g.Group("")
	authGroup.Use(middleware.JWTAuthMiddleware())
	{
		authGroup.GET("/users/me", app.UserHandler.GetMe)
		authGroup.POST("/users/update", app.UserHandler.UpdateUserHandle)
		authGroup.POST("/follow", app.FollowHandler.CreateFollowAuth)
		authGroup.GET("/dm/peers", app.DMHandler.ListPeers)
		authGroup.GET("/dm/messages", app.DMHandler.ListMessages)
		authGroup.POST("/dm/messages", app.DMHandler.SendMessage)
		authGroup.GET("/dm/unread_count", app.DMHandler.GetUnreadCount)
		authGroup.POST("/dm/unread_clear", app.DMHandler.ClearUnread)
		authGroup.POST("/dm/conversation/delete", app.DMHandler.DeleteConversation)
		authGroup.POST("/articles", app.ArticleHandler.CreateArticleHandle)
		authGroup.PUT("/articles/:id", app.ArticleHandler.UpdateArticleHandle)
		authGroup.GET("/articles", app.ArticleHandler.GetArticleListHandler)
		authGroup.GET("/articles/following_latest", app.ArticleHandler.GetFollowingLatestArticles)
		authGroup.POST("/articles/like", app.ArticleHandler.LikeArticleHandle)
		authGroup.DELETE("/articles/:id", app.ArticleHandler.DeleteArticleHandle)
		authGroup.POST("/articles/comments", app.CommentHandler.CreateComment)
		authGroup.DELETE("/articles/comments/:id", app.CommentHandler.DeleteComment)

		authGroup.POST("/topics", app.TopicHandler.CreateTopicAuth)
		authGroup.POST("/topics/:id/discussions", app.TopicHandler.CreateTopicDiscussionAuth)
		authGroup.DELETE("/topics/:id", app.TopicHandler.DeleteTemporaryTopic)
	}
}
