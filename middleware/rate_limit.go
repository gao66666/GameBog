package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type clientBucket struct {
	tokens     float64
	lastRefill time.Time
}

func RateLimit(perMinute int, burst int) gin.HandlerFunc {
	if perMinute <= 0 {
		perMinute = 120
	}
	if burst <= 0 {
		burst = 20
	}

	var (
		mu      sync.Mutex
		clients = make(map[string]*clientBucket)
		rate    = float64(perMinute) / 60.0
	)

	return func(c *gin.Context) {
		ip := c.ClientIP()
		now := time.Now()

		mu.Lock()
		bucket, ok := clients[ip]
		if !ok {
			bucket = &clientBucket{tokens: float64(burst), lastRefill: now}
			clients[ip] = bucket
		}

		elapsed := now.Sub(bucket.lastRefill).Seconds()
		bucket.tokens += elapsed * rate
		if bucket.tokens > float64(burst) {
			bucket.tokens = float64(burst)
		}
		bucket.lastRefill = now

		if bucket.tokens < 1 {
			mu.Unlock()
			c.JSON(http.StatusTooManyRequests, gin.H{"code": 42900, "message": "rate limit exceeded"})
			c.Abort()
			return
		}

		bucket.tokens--
		mu.Unlock()
		c.Next()
	}
}
