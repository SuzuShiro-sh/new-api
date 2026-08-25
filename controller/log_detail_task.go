// 本文件提供请求响应详情过期清理与全量回收任务接口。
package controller

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

func CreateLogDetailCleanupSystemTask(c *gin.Context) {
	targetTimestamp, err := strconv.ParseInt(c.Query("target_timestamp"), 10, 64)
	if err != nil || targetTimestamp <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "target timestamp is required",
		})
		return
	}
	reclaimSpace, _ := strconv.ParseBool(c.Query("reclaim_space"))
	task, err := service.StartLogDetailCleanupTask(targetTimestamp, reclaimSpace)
	respondLogDetailTask(c, task, err)
}

func CreateLogDetailClearAllSystemTask(c *gin.Context) {
	task, err := service.StartLogDetailClearAllTask()
	respondLogDetailTask(c, task, err)
}

func respondLogDetailTask(c *gin.Context, task *model.SystemTask, err error) {
	if err == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"message": "",
			"data":    task.ToResponse(),
		})
		return
	}

	conflict := &service.LogDetailCleanupConflictError{}
	if errors.As(err, &conflict) {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": err.Error(),
			"data": gin.H{
				"requested_mode": conflict.RequestedMode,
				"active_mode":    conflict.ActiveMode,
			},
		})
		return
	}
	if errors.Is(err, model.ErrLogDetailStoreUnavailable) {
		c.JSON(http.StatusConflict, gin.H{
			"success": false,
			"message": err.Error(),
		})
		return
	}
	common.ApiError(c, err)
}
