// 本文件把定时与手工请求响应详情维护统一到 system-task 租约中执行。
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

const (
	logDetailCleanupBatchSize = 500
	logDetailCleanupInterval  = time.Hour
)

// LogDetailCleanupMode 区分按保留期清理与全部清空两种任务行为。
type LogDetailCleanupMode string

const (
	LogDetailCleanupModeExpired LogDetailCleanupMode = "expired"
	LogDetailCleanupModeAll     LogDetailCleanupMode = "all"
)

// LogDetailCleanupPayload 描述详情清理范围与可选物理空间回收设置。
type LogDetailCleanupPayload struct {
	Mode            LogDetailCleanupMode `json:"mode,omitempty"`
	TargetTimestamp int64                `json:"target_timestamp"`
	BatchSize       int                  `json:"batch_size"`
	ReclaimSpace    bool                 `json:"reclaim_space"`
}

// LogDetailCleanupResult 区分数据删除、空间回收及部分成功状态。
type LogDetailCleanupResult struct {
	Mode           LogDetailCleanupMode `json:"mode"`
	DeletedCount   int64                `json:"deleted_count"`
	SpaceReclaimed bool                 `json:"space_reclaimed"`
	Partial        bool                 `json:"partial,omitempty"`
}

// LogDetailCleanupConflictError 表示另一种详情维护模式正在执行。
type LogDetailCleanupConflictError struct {
	RequestedMode LogDetailCleanupMode
	ActiveMode    LogDetailCleanupMode
}

func (err *LogDetailCleanupConflictError) Error() string {
	return fmt.Sprintf("log detail cleanup mode %s conflicts with active mode %s", err.RequestedMode, err.ActiveMode)
}

type logDetailCleanupHandler struct{}

func (logDetailCleanupHandler) Type() string { return model.SystemTaskTypeLogDetailCleanup }

func (logDetailCleanupHandler) Enabled() bool {
	return common.LogDetailRetentionDays > 0 && model.IsLogDetailStoreAvailable()
}

func (logDetailCleanupHandler) Interval() time.Duration { return logDetailCleanupInterval }

func (logDetailCleanupHandler) NewPayload() any {
	return LogDetailCleanupPayload{
		Mode:            LogDetailCleanupModeExpired,
		TargetTimestamp: common.GetTimestamp() - int64(common.LogDetailRetentionDays)*86400,
		BatchSize:       logDetailCleanupBatchSize,
	}
}

func (logDetailCleanupHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	runLogDetailCleanupTask(ctx, task, runnerID)
}

func init() {
	RegisterSystemTaskHandler(logDetailCleanupHandler{})
}

// StartLogDetailCleanupTask 创建详情清理任务，可选择在删除后整理物理空间。
func StartLogDetailCleanupTask(targetTimestamp int64, reclaimSpace bool) (*model.SystemTask, error) {
	if targetTimestamp <= 0 {
		return nil, errors.New("target timestamp is required")
	}
	return startLogDetailCleanupTask(LogDetailCleanupPayload{
		Mode:            LogDetailCleanupModeExpired,
		TargetTimestamp: targetTimestamp,
		BatchSize:       logDetailCleanupBatchSize,
		ReclaimSpace:    reclaimSpace,
	})
}

// StartLogDetailClearAllTask 创建清空全部详情并立即释放表空间的任务。
func StartLogDetailClearAllTask() (*model.SystemTask, error) {
	return startLogDetailCleanupTask(LogDetailCleanupPayload{
		Mode:         LogDetailCleanupModeAll,
		ReclaimSpace: true,
	})
}

func startLogDetailCleanupTask(payload LogDetailCleanupPayload) (*model.SystemTask, error) {
	if !model.IsLogDetailStoreAvailable() {
		return nil, model.ErrLogDetailStoreUnavailable
	}
	activeTask, err := model.GetActiveSystemTask(model.SystemTaskTypeLogDetailCleanup)
	if err != nil {
		return nil, err
	}
	if activeTask != nil {
		return reuseMatchingLogDetailTask(activeTask, payload.Mode)
	}

	task, err := model.CreateSystemTask(model.SystemTaskTypeLogDetailCleanup, payload, LogCleanupState{})
	if err != nil {
		activeTask, activeErr := model.GetActiveSystemTask(model.SystemTaskTypeLogDetailCleanup)
		if activeErr == nil && activeTask != nil {
			return reuseMatchingLogDetailTask(activeTask, payload.Mode)
		}
		return nil, err
	}
	notifySystemTaskRunner()
	return task, nil
}

func reuseMatchingLogDetailTask(task *model.SystemTask, requestedMode LogDetailCleanupMode) (*model.SystemTask, error) {
	payload := LogDetailCleanupPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		return nil, fmt.Errorf("failed to decode active log detail cleanup task: %w", err)
	}
	if payload.Mode == "" {
		payload.Mode = LogDetailCleanupModeExpired
	}
	if payload.Mode != requestedMode {
		return nil, &LogDetailCleanupConflictError{
			RequestedMode: requestedMode,
			ActiveMode:    payload.Mode,
		}
	}
	return task, nil
}

func runLogDetailCleanupTask(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := LogDetailCleanupPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	if payload.Mode == "" {
		payload.Mode = LogDetailCleanupModeExpired
	}
	if payload.Mode != LogDetailCleanupModeExpired && payload.Mode != LogDetailCleanupModeAll {
		failSystemTask(task, runnerID, fmt.Errorf("unsupported log detail cleanup mode: %s", payload.Mode))
		return
	}

	state := LogCleanupState{}
	if err := task.DecodeState(&state); err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	if payload.Mode == LogDetailCleanupModeAll {
		runLogDetailClearAllTask(ctx, task, runnerID, &state)
		return
	}
	runExpiredLogDetailCleanupTask(ctx, task, runnerID, payload, &state)
}

func runLogDetailClearAllTask(ctx context.Context, task *model.SystemTask, runnerID string, state *LogCleanupState) {
	total, err := model.CountLogDetails(ctx)
	if err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	state.Total = total
	state.Remaining = total
	state.Progress = logCleanupProgress(state.Processed, state.Total)
	if err = model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		logSystemTaskLockError(ctx, task, err)
		return
	}

	clearResult, clearErr := model.ClearAllLogDetailsAndReclaim(ctx)
	state.Processed = clearResult.DeletedCount
	if state.Total < state.Processed {
		state.Total = state.Processed
	}
	state.Remaining = 0
	state.Progress = 100
	result := LogDetailCleanupResult{
		Mode:           LogDetailCleanupModeAll,
		DeletedCount:   clearResult.DeletedCount,
		SpaceReclaimed: clearResult.SpaceReclaimed,
		Partial:        clearErr != nil && clearResult.DeletedCount > 0,
	}
	if clearErr != nil {
		_ = model.UpdateSystemTaskState(task.TaskID, runnerID, state)
		finishLogDetailCleanupFailure(task, runnerID, result, clearErr)
		return
	}
	if err = model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		logSystemTaskLockError(ctx, task, err)
		return
	}
	if err = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, ""); err != nil {
		logSystemTaskLockError(ctx, task, err)
	}
}

func runExpiredLogDetailCleanupTask(ctx context.Context, task *model.SystemTask, runnerID string, payload LogDetailCleanupPayload, state *LogCleanupState) {
	if payload.TargetTimestamp <= 0 {
		failSystemTask(task, runnerID, errors.New("target timestamp is required"))
		return
	}
	if payload.BatchSize <= 0 {
		payload.BatchSize = logDetailCleanupBatchSize
	}

	remaining, err := model.CountExpiredLogDetails(ctx, payload.TargetTimestamp)
	if err != nil {
		failSystemTask(task, runnerID, err)
		return
	}
	syncLogCleanupStateFromRemaining(state, remaining)
	if err = model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		logSystemTaskLockError(ctx, task, err)
		return
	}

	for state.Remaining > 0 {
		rowsAffected, deleteErr := model.DeleteExpiredLogDetailsBatch(ctx, payload.TargetTimestamp, payload.BatchSize)
		if deleteErr != nil {
			failSystemTask(task, runnerID, deleteErr)
			return
		}
		if rowsAffected == 0 {
			remaining, deleteErr = model.CountExpiredLogDetails(ctx, payload.TargetTimestamp)
			if deleteErr != nil {
				failSystemTask(task, runnerID, deleteErr)
				return
			}
			if remaining > 0 {
				failSystemTask(task, runnerID, errors.New("no log detail rows were deleted"))
				return
			}
			state.Remaining = 0
			break
		}

		state.Processed += rowsAffected
		if state.Remaining > rowsAffected {
			state.Remaining -= rowsAffected
		} else {
			state.Remaining = 0
		}
		state.Progress = logCleanupProgress(state.Processed, state.Total)
		if err = model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
			logSystemTaskLockError(ctx, task, err)
			return
		}
	}

	result := LogDetailCleanupResult{
		Mode:         LogDetailCleanupModeExpired,
		DeletedCount: state.Processed,
	}
	if payload.ReclaimSpace {
		if err = model.ReclaimLogDetailStorage(ctx); err != nil {
			result.Partial = state.Processed > 0
			finishLogDetailCleanupFailure(task, runnerID, result, err)
			return
		}
		result.SpaceReclaimed = true
	}

	state.Progress = 100
	state.Remaining = 0
	if err = model.UpdateSystemTaskState(task.TaskID, runnerID, state); err != nil {
		logSystemTaskLockError(ctx, task, err)
		return
	}
	if err = model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusSucceeded, result, ""); err != nil {
		logSystemTaskLockError(ctx, task, err)
	}
}

func finishLogDetailCleanupFailure(task *model.SystemTask, runnerID string, result LogDetailCleanupResult, err error) {
	logger.LogWarn(context.Background(), fmt.Sprintf("system task %s failed: %v", task.TaskID, err))
	if finishErr := model.FinishSystemTask(task.TaskID, runnerID, model.SystemTaskStatusFailed, result, err.Error()); finishErr != nil {
		logger.LogWarn(context.Background(), fmt.Sprintf("system task %s failed to save failure state: %v", task.TaskID, finishErr))
	}
}
