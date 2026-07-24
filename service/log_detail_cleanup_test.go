package service

import (
	"context"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCleanupExpiredLogDetailsUsesConfiguredRetention(t *testing.T) {
	previousRetentionDays := common.LogDetailRetentionDays
	common.LogDetailRetentionDays = 3
	t.Cleanup(func() {
		common.LogDetailRetentionDays = previousRetentionDays
	})
	require.NoError(t, model.LOG_DB.Exec("DELETE FROM log_details").Error)
	t.Cleanup(func() {
		_ = model.LOG_DB.Exec("DELETE FROM log_details").Error
	})

	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	require.NoError(t, model.LOG_DB.Create(&model.LogDetail{
		RequestId: "req_expired",
		UserId:    1,
		CreatedAt: now.AddDate(0, 0, -4).Unix(),
	}).Error)
	require.NoError(t, model.LOG_DB.Create(&model.LogDetail{
		RequestId: "req_recent",
		UserId:    1,
		CreatedAt: now.AddDate(0, 0, -2).Unix(),
	}).Error)

	deleted, err := cleanupExpiredLogDetails(context.Background(), now)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	var remaining []model.LogDetail
	require.NoError(t, model.LOG_DB.Order("request_id").Find(&remaining).Error)
	require.Len(t, remaining, 1)
	assert.Equal(t, "req_recent", remaining[0].RequestId)
}
