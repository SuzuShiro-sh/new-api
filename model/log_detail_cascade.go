// 本文件在普通日志批量清理时同步删除关联的请求响应详情。
package model

import (
	"context"

	"gorm.io/gorm"
)

func deleteOldLogBatchWithDetails(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if !LOG_DB.Migrator().HasTable(&LogDetail{}) {
		result := LOG_DB.WithContext(ctx).Where("created_at < ?", targetTimestamp).Limit(limit).Delete(&Log{})
		return result.RowsAffected, result.Error
	}

	var logs []Log
	if err := LOG_DB.WithContext(ctx).
		Select("id", "request_id").
		Where("created_at < ?", targetTimestamp).
		Order("id").
		Limit(limit).
		Find(&logs).Error; err != nil {
		return 0, err
	}
	if len(logs) == 0 {
		return 0, nil
	}

	var deleted int64
	err := LOG_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		requestIDs := make([]string, 0, len(logs))
		logIDs := make([]int, 0, len(logs))
		for _, log := range logs {
			logIDs = append(logIDs, log.Id)
			if log.RequestId != "" {
				requestIDs = append(requestIDs, log.RequestId)
			}
		}
		if len(requestIDs) > 0 {
			if err := tx.Where("request_id IN ?", requestIDs).Delete(&LogDetail{}).Error; err != nil {
				return err
			}
		}
		result := tx.Where("id IN ?", logIDs).Delete(&Log{})
		deleted = result.RowsAffected
		return result.Error
	})
	return deleted, err
}
