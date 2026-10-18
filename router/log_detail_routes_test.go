// 本文件验证请求响应详情的查询与维护路由已注册。
package router

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestLogDetailRoutesAreRegistered(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	SetApiRouter(engine)
	routes := make(map[string]struct{}, len(engine.Routes()))
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = struct{}{}
	}
	assert.Contains(t, routes, http.MethodGet+" /api/log/detail/:request_id")
	assert.Contains(t, routes, http.MethodGet+" /api/log/self/detail/:request_id")
	assert.Contains(t, routes, http.MethodPost+" /api/system-task/log-detail-cleanup")
	assert.Contains(t, routes, http.MethodPost+" /api/system-task/log-detail-clear-all")
}
