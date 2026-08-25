// 本文件集中注册请求响应详情的查询与维护路由。
package router

import (
	"github.com/QuantumNous/new-api/controller"
	"github.com/QuantumNous/new-api/middleware"

	"github.com/gin-gonic/gin"
)

func registerLogDetailRoutes(logRoute *gin.RouterGroup, systemTaskRoute *gin.RouterGroup) {
	logRoute.GET("/detail/:request_id", middleware.AdminAuth(), controller.GetLogDetail)
	logRoute.GET("/self/detail/:request_id", middleware.UserAuth(), controller.GetUserLogDetail)
	systemTaskRoute.POST("/log-detail-cleanup", controller.CreateLogDetailCleanupSystemTask)
	systemTaskRoute.POST("/log-detail-clear-all", controller.CreateLogDetailClearAllSystemTask)
}
