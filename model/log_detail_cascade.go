// 本文件在普通日志批量清理时同步删除关联的请求响应详情。
package model

import (
	"context"
	"slices"

	"gorm.io/gorm"
)

func deleteOldLogBatchWithDetails(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	tables, err := LOG_DB.WithContext(ctx).Migrator().GetTables()
	if err != nil {
		return 0, err
	}
	hasDetails := slices.Contains(tables, "log_details")
	hasArchive := slices.Contains(tables, "log_detail_archive_chunks")
	var deleted int64
	err = LOG_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 与详情写入保持相同的锁顺序：先锁普通日志，再操作详情。
		// 否则删除详情后仍可能有并发请求写入，留下普通日志已删除的孤立正文。
		var logs []Log
		if err := lockForUpdate(tx).Select("id", "request_id").
			Where("created_at < ?", targetTimestamp).
			Order("id").Limit(limit).Find(&logs).Error; err != nil {
			return err
		}
		if len(logs) == 0 {
			return nil
		}
		requestIDs := make([]string, 0, len(logs))
		logIDs := make([]int, 0, len(logs))
		for _, log := range logs {
			logIDs = append(logIDs, log.Id)
			if log.RequestId != "" {
				requestIDs = append(requestIDs, log.RequestId)
			}
		}
		result := tx.Where("id IN ?", logIDs).Delete(&Log{})
		deleted = result.RowsAffected
		if result.Error != nil {
			return result.Error
		}
		if hasDetails && len(requestIDs) > 0 {
			// 重试会产生共用请求 ID 的多条日志，最后一条关联日志删除后才清理正文。
			remaining := tx.Model(&Log{}).Select("request_id").Where("request_id IN ?", requestIDs)
			if hasArchive {
				if err := tx.Where("request_id IN ?", requestIDs).Where("request_id NOT IN (?)", remaining).Delete(&LogDetailArchiveChunk{}).Error; err != nil {
					return err
				}
			}
			return tx.Where("request_id IN ?", requestIDs).
				Where("request_id NOT IN (?)", remaining).Delete(&LogDetail{}).Error
		}
		return nil
	})
	return deleted, err
}
