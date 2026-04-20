package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func HomePage(c *gin.Context) {
	c.HTML(http.StatusOK, "home.tmpl", gin.H{})
}

func LoginPage(c *gin.Context) {
	c.HTML(http.StatusOK, "login.tmpl", gin.H{})
}

func MePage(c *gin.Context) {
	c.HTML(http.StatusOK, "me.tmpl", gin.H{})
}

func UserPage(c *gin.Context) {
	c.HTML(http.StatusOK, "user.tmpl", gin.H{
		"UserID": c.Param("id"),
	})
}

func DMPage(c *gin.Context) {
	c.HTML(http.StatusOK, "dm.tmpl", gin.H{
		"PeerID": c.Query("peer_id"),
	})
}

func ArticlePage(c *gin.Context) {
	c.HTML(http.StatusOK, "article.tmpl", gin.H{
		"ArticleID": c.Param("id"),
	})
}

func EditorPage(c *gin.Context) {
	c.HTML(http.StatusOK, "editor.tmpl", gin.H{
		"ArticleID": c.Query("id"),
	})
}

func TopicsPage(c *gin.Context) {
	c.HTML(http.StatusOK, "topics.tmpl", gin.H{})
}

func TopicPage(c *gin.Context) {
	c.HTML(http.StatusOK, "topic.tmpl", gin.H{
		"TopicID": c.Param("id"),
	})
}

func TopicDiscussPage(c *gin.Context) {
	c.HTML(http.StatusOK, "topic_discuss.tmpl", gin.H{
		"TopicID": c.Param("id"),
	})
}
