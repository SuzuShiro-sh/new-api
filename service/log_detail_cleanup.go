package service

import (
	"context"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

const (
	logDetailCleanupInterval  = time.Hour
	logDetailCleanupBatchSize = 500
)

// StartLogDetailRetentionCleanup 启动主节点上的详情保留期清理任务.
func StartLogDetailRetentionCleanup() {
	if !common.IsMasterNode {
		return
	}
	go func() {
		cleanupExpiredLogDetailsOnce()
		ticker := time.NewTicker(logDetailCleanupInterval)
		defer ticker.Stop()
		for range ticker.C {
			cleanupExpiredLogDetailsOnce()
		}
	}()
}

func cleanupExpiredLogDetailsOnce() {
	deleted, err := cleanupExpiredLogDetails(context.Background(), time.Now())
	if err != nil {
		common.SysError("failed to clean expired log details: " + err.Error())
		return
	}
	if deleted > 0 {
		common.SysLog(fmt.Sprintf("cleaned %d expired log details", deleted))
	}
}

func cleanupExpiredLogDetails(ctx context.Context, now time.Time) (int64, error) {
	retentionDays := common.LogDetailRetentionDays
	if retentionDays <= 0 || common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		return 0, nil
	}
	targetTimestamp := now.AddDate(0, 0, -retentionDays).Unix()
	var deleted int64
	for {
		rowsAffected, err := model.DeleteExpiredLogDetailsBatch(ctx, targetTimestamp, logDetailCleanupBatchSize)
		if err != nil {
			return deleted, err
		}
		deleted += rowsAffected
		if rowsAffected == 0 {
			return deleted, nil
		}
	}
}
