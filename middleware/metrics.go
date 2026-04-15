package middleware

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type metricCounter struct {
	Count       int64
	TotalMicros int64
	Status2xx   int64
	Status4xx   int64
	Status5xx   int64
}

var (
	metricsMu      sync.RWMutex
	requestMetrics = make(map[string]*metricCounter)
)

func Metrics() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		key := c.Request.Method + " " + c.FullPath()
		if c.FullPath() == "" {
			key = c.Request.Method + " /unknown"
		}

		costMicros := time.Since(start).Microseconds()
		status := c.Writer.Status()

		metricsMu.Lock()
		m, ok := requestMetrics[key]
		if !ok {
			m = &metricCounter{}
			requestMetrics[key] = m
		}
		m.Count++
		m.TotalMicros += costMicros
		switch {
		case status >= 200 && status < 300:
			m.Status2xx++
		case status >= 400 && status < 500:
			m.Status4xx++
		case status >= 500:
			m.Status5xx++
		}
		metricsMu.Unlock()
	}
}

func MetricsHandler(c *gin.Context) {
	metricsMu.RLock()
	keys := make([]string, 0, len(requestMetrics))
	for k := range requestMetrics {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("# HELP goblog_http_requests_total Total HTTP requests by route\n")
	sb.WriteString("# TYPE goblog_http_requests_total counter\n")
	sb.WriteString("# HELP goblog_http_request_duration_microseconds_total Sum of request duration in microseconds\n")
	sb.WriteString("# TYPE goblog_http_request_duration_microseconds_total counter\n")
	sb.WriteString("# HELP goblog_http_responses_total Response status class counters\n")
	sb.WriteString("# TYPE goblog_http_responses_total counter\n")

	for _, route := range keys {
		m := requestMetrics[route]
		labelRoute := strings.ReplaceAll(route, "\"", "")
		sb.WriteString(fmt.Sprintf("goblog_http_requests_total{route=\"%s\"} %d\n", labelRoute, m.Count))
		sb.WriteString(fmt.Sprintf("goblog_http_request_duration_microseconds_total{route=\"%s\"} %d\n", labelRoute, m.TotalMicros))
		sb.WriteString(fmt.Sprintf("goblog_http_responses_total{route=\"%s\",class=\"2xx\"} %d\n", labelRoute, m.Status2xx))
		sb.WriteString(fmt.Sprintf("goblog_http_responses_total{route=\"%s\",class=\"4xx\"} %d\n", labelRoute, m.Status4xx))
		sb.WriteString(fmt.Sprintf("goblog_http_responses_total{route=\"%s\",class=\"5xx\"} %d\n", labelRoute, m.Status5xx))
	}
	metricsMu.RUnlock()

	c.Data(http.StatusOK, "text/plain; version=0.0.4", []byte(sb.String()))
}
