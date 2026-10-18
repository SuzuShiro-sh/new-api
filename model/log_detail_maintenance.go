// 本文件负责请求响应详情的过期删除、全量清空与跨数据库空间回收。
package model

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

// LogDetailClearResult 区分已删除数据与物理空间回收结果。
type LogDetailClearResult struct {
	DeletedCount   int64
	SpaceReclaimed bool
}

// mySQLTableMaintenanceResult 保存 MySQL/MariaDB 表维护语句返回的逐行状态。
type mySQLTableMaintenanceResult struct {
	TableName   string `gorm:"column:Table"`
	Operation   string `gorm:"column:Op"`
	MessageType string `gorm:"column:Msg_type"`
	MessageText string `gorm:"column:Msg_text"`
}

func requireLogDetailStore() error {
	if !IsLogDetailStoreAvailable() || !LOG_DB.Migrator().HasTable(&LogDetail{}) {
		return ErrLogDetailStoreUnavailable
	}
	return nil
}

// CountExpiredLogDetails 统计指定时间之前的详情记录数量。
func CountExpiredLogDetails(ctx context.Context, targetTimestamp int64) (int64, error) {
	if err := requireLogDetailStore(); err != nil {
		return 0, err
	}
	var total int64
	if err := LOG_DB.WithContext(ctx).Model(&LogDetail{}).Where("created_at < ?", targetTimestamp).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// CountLogDetails 统计当前保存的全部请求响应详情。
func CountLogDetails(ctx context.Context) (int64, error) {
	if err := requireLogDetailStore(); err != nil {
		return 0, err
	}
	var total int64
	if err := LOG_DB.WithContext(ctx).Model(&LogDetail{}).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// DeleteExpiredLogDetailsBatch 分批删除指定时间之前的详情记录。
func DeleteExpiredLogDetailsBatch(ctx context.Context, targetTimestamp int64, limit int) (int64, error) {
	if err := requireLogDetailStore(); err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if limit <= 0 {
		limit = 500
	}

	var ids []int
	if err := LOG_DB.WithContext(ctx).Model(&LogDetail{}).
		Where("created_at < ?", targetTimestamp).
		Order("id").
		Limit(limit).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := LOG_DB.WithContext(ctx).Where("id IN ?", ids).Delete(&LogDetail{})
	if result.Error != nil {
		return 0, result.Error
	}
	return result.RowsAffected, nil
}

// ClearAllLogDetailsAndReclaim 删除全部详情，并独立报告空间回收是否成功。
func ClearAllLogDetailsAndReclaim(ctx context.Context) (LogDetailClearResult, error) {
	result := LogDetailClearResult{}
	if err := requireLogDetailStore(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}

	deleted := LOG_DB.WithContext(ctx).
		Session(&gorm.Session{AllowGlobalUpdate: true}).
		Delete(&LogDetail{})
	if deleted.Error != nil {
		return result, fmt.Errorf("failed to clear log detail table: %w", deleted.Error)
	}
	result.DeletedCount = deleted.RowsAffected

	if err := ReclaimLogDetailStorage(ctx); err != nil {
		return result, fmt.Errorf("log details were cleared but storage reclaim failed: %w", err)
	}
	result.SpaceReclaimed = true
	return result, nil
}

// ReclaimLogDetailStorage 整理详情表并把可回收空间归还给操作系统。
func ReclaimLogDetailStorage(ctx context.Context) error {
	if err := requireLogDetailStore(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	db := LOG_DB.WithContext(ctx)
	switch LOG_DB.Dialector.Name() {
	case "sqlite":
		if err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)").Error; err != nil {
			return fmt.Errorf("failed to truncate SQLite WAL before reclaim: %w", err)
		}
		if err := db.Exec("VACUUM").Error; err != nil {
			return fmt.Errorf("failed to vacuum SQLite log database: %w", err)
		}
	case "mysql":
		var results []mySQLTableMaintenanceResult
		if err := db.Raw("OPTIMIZE TABLE `log_details`").Scan(&results).Error; err != nil {
			return fmt.Errorf("failed to optimize MySQL log detail table: %w", err)
		}
		if err := validateMySQLTableMaintenanceResults(results); err != nil {
			return fmt.Errorf("failed to optimize MySQL log detail table: %w", err)
		}
	case "postgres":
		sqlDB, err := LOG_DB.DB()
		if err != nil {
			return fmt.Errorf("failed to access PostgreSQL log database: %w", err)
		}
		conn, err := sqlDB.Conn(ctx)
		if err != nil {
			return fmt.Errorf("failed to acquire PostgreSQL log connection: %w", err)
		}
		defer conn.Close()
		if _, err = conn.ExecContext(ctx, `VACUUM (FULL, ANALYZE) "log_details"`); err != nil {
			return fmt.Errorf("failed to vacuum PostgreSQL log detail table: %w", err)
		}
	default:
		return fmt.Errorf("unsupported log database dialect: %s", LOG_DB.Dialector.Name())
	}
	return nil
}

// validateMySQLTableMaintenanceResults 确保表维护结果包含最终成功状态且没有表级错误。
func validateMySQLTableMaintenanceResults(results []mySQLTableMaintenanceResult) error {
	if len(results) == 0 {
		return errors.New("OPTIMIZE TABLE returned no result")
	}

	statusOK := false
	failures := make([]string, 0, 1)
	messages := make([]string, 0, len(results))
	for _, result := range results {
		messageType := strings.TrimSpace(result.MessageType)
		messageText := strings.TrimSpace(result.MessageText)
		if messageType == "" && messageText == "" {
			continue
		}
		messages = append(messages, fmt.Sprintf("%s: %s", messageType, messageText))
		switch {
		case strings.EqualFold(messageType, "error"):
			failures = append(failures, messageText)
		case strings.EqualFold(messageType, "status") && strings.EqualFold(messageText, "OK"):
			statusOK = true
		case strings.EqualFold(messageType, "status"):
			failures = append(failures, messageText)
		}
	}

	if len(failures) > 0 {
		return fmt.Errorf("OPTIMIZE TABLE reported failure: %s", strings.Join(failures, "; "))
	}
	if !statusOK {
		return fmt.Errorf("OPTIMIZE TABLE did not report status OK: %s", strings.Join(messages, "; "))
	}
	return nil
}
