package model

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDeleteOldLogBatchDeletesMatchingLogDetails(t *testing.T) {
	truncateTables(t)

	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		CreatedAt: 10,
		Type:      LogTypeConsume,
		RequestId: "req_old",
	}).Error)
	require.NoError(t, LOG_DB.Create(&LogDetail{
		RequestId: "req_old",
		UserId:    1,
		CreatedAt: 10,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		CreatedAt: 30,
		Type:      LogTypeConsume,
		RequestId: "req_new",
	}).Error)
	require.NoError(t, LOG_DB.Create(&LogDetail{
		RequestId: "req_new",
		UserId:    1,
		CreatedAt: 30,
	}).Error)

	deleted, err := DeleteOldLogBatch(context.Background(), 20, 100)
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	var oldDetailCount int64
	require.NoError(t, LOG_DB.Model(&LogDetail{}).Where("request_id = ?", "req_old").Count(&oldDetailCount).Error)
	require.Equal(t, int64(0), oldDetailCount)

	var newDetailCount int64
	require.NoError(t, LOG_DB.Model(&LogDetail{}).Where("request_id = ?", "req_new").Count(&newDetailCount).Error)
	require.Equal(t, int64(1), newDetailCount)
}
