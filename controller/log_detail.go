// 本文件提供请求响应详情的管理员与用户授权查询接口。
package controller

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func GetLogDetail(c *gin.Context) {
	respondLogDetail(c, true)
}

func GetUserLogDetail(c *gin.Context) {
	respondLogDetail(c, false)
}

func respondLogDetail(c *gin.Context, isAdmin bool) {
	detail, err := service.GetLogDetail(c, c.Param("request_id"), isAdmin)
	if err == nil {
		common.ApiSuccess(c, detail)
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, model.ErrLogDetailStoreUnavailable) {
		message := "日志详情不存在"
		if !isAdmin {
			message = "日志详情不存在或无权查看"
		}
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": message,
		})
		return
	}
	common.ApiError(c, err)
}
