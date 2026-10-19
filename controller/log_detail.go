// 本文件提供请求响应详情的管理员与用户授权查询接口。
package controller

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
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
		if isAdmin && c.Query("format") == "archive" {
			if detail.ArchiveBytes == 0 {
				common.ApiErrorT(c, "Log detail not found")
				return
			}
			// 先校验并缓存到磁盘，避免清理竞态或数据库错误在发送 200 后留下损坏文件。
			_, file, exportErr := common.CreateDiskCacheFile(common.DiskCacheTypeLogDetail)
			if exportErr != nil {
				common.ApiError(c, exportErr)
				return
			}
			defer func() {
				if closeErr := file.Close(); closeErr != nil {
					logger.LogError(c, "close log detail export: "+closeErr.Error())
				}
				if removeErr := os.Remove(file.Name()); removeErr != nil {
					logger.LogError(c, "remove log detail export: "+removeErr.Error())
				}
			}()
			if exportErr = model.WriteLogDetailArchive(c.Request.Context(), detail.RequestId, detail.ArchiveBytes, file); exportErr == nil {
				_, exportErr = file.Seek(0, io.SeekStart)
			}
			if exportErr != nil {
				common.ApiError(c, exportErr)
				return
			}
			c.Header("Content-Type", "application/gzip")
			c.Header("Cache-Control", "no-store")
			c.Header("Content-Length", strconv.FormatInt(detail.ArchiveBytes, 10))
			c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="request-detail-%d.ndjson.gz"`, detail.Id))
			c.Status(http.StatusOK)
			if _, err := io.CopyBuffer(c.Writer, file, make([]byte, 32*1024)); err != nil {
				logger.LogError(c, "export log detail archive: "+err.Error())
			}
			return
		}
		common.ApiSuccess(c, detail)
		return
	}
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, model.ErrLogDetailStoreUnavailable) {
		message := "Log detail not found"
		if !isAdmin {
			message = "Log detail not found or access denied"
		}
		common.ApiErrorT(c, message)
		return
	}
	common.ApiError(c, err)
}
