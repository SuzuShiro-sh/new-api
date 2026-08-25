// 本文件验证请求响应详情维护任务的模式互斥与清理结果。
package service

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStartLogDetailClearAllRejectsDifferentActiveMode(t *testing.T) {
	truncate(t)
	active, err := StartLogDetailCleanupTask(20, false)
	require.NoError(t, err)

	clearAll, err := StartLogDetailClearAllTask()
	assert.Nil(t, clearAll)
	conflict := &LogDetailCleanupConflictError{}
	require.ErrorAs(t, err, &conflict)
	assert.Equal(t, LogDetailCleanupModeAll, conflict.RequestedMode)
	assert.Equal(t, LogDetailCleanupModeExpired, conflict.ActiveMode)

	reused, err := StartLogDetailCleanupTask(30, true)
	require.NoError(t, err)
	assert.Equal(t, active.TaskID, reused.TaskID)
}

func TestScheduledLogDetailCleanupUsesCurrentRetention(t *testing.T) {
	originalRetention := common.LogDetailRetentionDays
	t.Cleanup(func() { common.LogDetailRetentionDays = originalRetention })
	common.LogDetailRetentionDays = 7

	handler := logDetailCleanupHandler{}
	require.True(t, handler.Enabled())
	payload, ok := handler.NewPayload().(LogDetailCleanupPayload)
	require.True(t, ok)
	assert.Equal(t, LogDetailCleanupModeExpired, payload.Mode)
	assert.Equal(t, logDetailCleanupBatchSize, payload.BatchSize)
	assert.InDelta(t, common.GetTimestamp()-7*86400, payload.TargetTimestamp, 1)
}

func TestRunLogDetailCleanupTaskPreservesUsageLogs(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create([]*model.Log{
		{UserId: 1, CreatedAt: 10, Type: model.LogTypeConsume, RequestId: "req_detail_old"},
		{UserId: 1, CreatedAt: 30, Type: model.LogTypeConsume, RequestId: "req_detail_new"},
	}).Error)
	require.NoError(t, model.LOG_DB.Create([]*model.LogDetail{
		{RequestId: "req_detail_old", UserId: 1, CreatedAt: 10, RequestBody: "old"},
		{RequestId: "req_detail_new", UserId: 1, CreatedAt: 30, RequestBody: "new"},
	}).Error)

	task := claimLogDetailTask(t, LogDetailCleanupPayload{
		Mode:            LogDetailCleanupModeExpired,
		TargetTimestamp: 20,
		BatchSize:       1,
	}, "runner-log-detail-cleanup")
	runLogDetailCleanupTask(context.Background(), task, "runner-log-detail-cleanup")

	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, finished)
	assert.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
	var result LogDetailCleanupResult
	require.NoError(t, common.UnmarshalJsonStr(finished.Result, &result))
	assert.Equal(t, LogDetailCleanupModeExpired, result.Mode)
	assert.Equal(t, int64(1), result.DeletedCount)
	assert.False(t, result.SpaceReclaimed)

	var detailRequestIDs []string
	require.NoError(t, model.LOG_DB.Model(&model.LogDetail{}).
		Order("request_id").
		Pluck("request_id", &detailRequestIDs).Error)
	assert.Equal(t, []string{"req_detail_new"}, detailRequestIDs)

	var logCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).
		Where("request_id IN ?", []string{"req_detail_old", "req_detail_new"}).
		Count(&logCount).Error)
	assert.Equal(t, int64(2), logCount)
}

func TestRunLogDetailClearAllTaskReportsActualDeletedRows(t *testing.T) {
	truncate(t)
	require.NoError(t, model.DB.Create([]*model.Log{
		{UserId: 1, CreatedAt: 10, Type: model.LogTypeConsume, RequestId: "req_detail_first"},
		{UserId: 1, CreatedAt: 30, Type: model.LogTypeConsume, RequestId: "req_detail_second"},
	}).Error)
	require.NoError(t, model.LOG_DB.Create([]*model.LogDetail{
		{RequestId: "req_detail_first", UserId: 1, CreatedAt: 10, RequestBody: "first"},
		{RequestId: "req_detail_second", UserId: 1, CreatedAt: 30, RequestBody: "second"},
	}).Error)

	task := claimLogDetailTask(t, LogDetailCleanupPayload{
		Mode:         LogDetailCleanupModeAll,
		ReclaimSpace: true,
	}, "runner-log-detail-clear-all")
	runLogDetailCleanupTask(context.Background(), task, "runner-log-detail-clear-all")

	finished, err := model.GetSystemTaskByTaskID(task.TaskID)
	require.NoError(t, err)
	require.NotNil(t, finished)
	assert.Equal(t, model.SystemTaskStatusSucceeded, finished.Status)
	var result LogDetailCleanupResult
	require.NoError(t, common.UnmarshalJsonStr(finished.Result, &result))
	assert.Equal(t, LogDetailCleanupModeAll, result.Mode)
	assert.Equal(t, int64(2), result.DeletedCount)
	assert.True(t, result.SpaceReclaimed)
	assert.False(t, result.Partial)

	detailCount, err := model.CountLogDetails(context.Background())
	require.NoError(t, err)
	assert.Zero(t, detailCount)
	var logCount int64
	require.NoError(t, model.LOG_DB.Model(&model.Log{}).
		Where("request_id IN ?", []string{"req_detail_first", "req_detail_second"}).
		Count(&logCount).Error)
	assert.Equal(t, int64(2), logCount)
}

func TestStartLogDetailTaskRejectsUnavailableStore(t *testing.T) {
	originalLogType := common.LogDatabaseType()
	t.Cleanup(func() { common.SetLogDatabaseType(originalLogType) })
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)

	task, err := StartLogDetailClearAllTask()
	assert.Nil(t, task)
	assert.True(t, errors.Is(err, model.ErrLogDetailStoreUnavailable))
}

func claimLogDetailTask(t *testing.T, payload LogDetailCleanupPayload, runnerID string) *model.SystemTask {
	t.Helper()
	task, err := model.CreateSystemTask(model.SystemTaskTypeLogDetailCleanup, payload, LogCleanupState{})
	require.NoError(t, err)
	claimedTask, claimed, err := model.ClaimSystemTask(
		task.ID,
		task.Type,
		runnerID,
		common.GetTimestamp()+60,
	)
	require.NoError(t, err)
	require.True(t, claimed)
	return claimedTask
}
