package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSaveLogDetailIfLogExistsCompressesAndLoadsPayload(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		CreatedAt: 10,
		Type:      LogTypeConsume,
		RequestId: "req_compressed",
	}).Error)

	requestBody := strings.Repeat(`{"prompt":"compressible"}`, 4096)
	responseBody := strings.Repeat(`{"text":"response"}`, 4096)
	detail := &LogDetail{
		RequestId:     "req_compressed",
		UserId:        1,
		CreatedAt:     10,
		UpdatedAt:     11,
		RequestBody:   LogDetailLargeText(requestBody),
		RequestParams: LogDetailLargeText(`{"model":"test"}`),
		ResponseBody:  LogDetailLargeText(responseBody),
	}

	saved, err := SaveLogDetailIfLogExists(context.Background(), detail)
	require.NoError(t, err)
	require.True(t, saved)

	var payloadVersion int
	var storedBytes int
	var legacyRequestBody string
	require.NoError(t, LOG_DB.Table("log_details").
		Select("payload_version", "stored_bytes", "request_body").
		Where("request_id = ?", "req_compressed").
		Row().Scan(&payloadVersion, &storedBytes, &legacyRequestBody))
	assert.Equal(t, logDetailPayloadVersion, payloadVersion)
	assert.Less(t, storedBytes, len(requestBody)+len(responseBody))
	assert.Empty(t, legacyRequestBody)

	loaded, err := GetLogDetailByRequestId("req_compressed")
	require.NoError(t, err)
	assert.Equal(t, requestBody, string(loaded.RequestBody))
	assert.Equal(t, responseBody, string(loaded.ResponseBody))
	assert.Equal(t, `{"model":"test"}`, string(loaded.RequestParams))
}

func TestSaveLogDetailIfLogExistsSkipsOrphanDetail(t *testing.T) {
	truncateTables(t)
	saved, err := SaveLogDetailIfLogExists(context.Background(), &LogDetail{
		RequestId:   "req_orphan",
		UserId:      1,
		CreatedAt:   10,
		RequestBody: "orphan",
	})
	require.NoError(t, err)
	assert.False(t, saved)

	var count int64
	require.NoError(t, LOG_DB.Model(&LogDetail{}).Where("request_id = ?", "req_orphan").Count(&count).Error)
	assert.Zero(t, count)
}

func TestGetLogDetailByRequestIdLoadsLegacyTextColumns(t *testing.T) {
	truncateTables(t)
	legacy := &LogDetail{
		RequestId:      "req_legacy_detail",
		UserId:         1,
		CreatedAt:      10,
		RequestBody:    `{"prompt":"legacy"}`,
		ResponseBody:   `{"text":"legacy response"}`,
		PayloadVersion: 0,
	}
	require.NoError(t, LOG_DB.Session(&gorm.Session{SkipHooks: true}).Create(legacy).Error)

	loaded, err := GetLogDetailByRequestId("req_legacy_detail")
	require.NoError(t, err)
	assert.Equal(t, legacy.RequestBody, loaded.RequestBody)
	assert.Equal(t, legacy.ResponseBody, loaded.ResponseBody)
	assert.Empty(t, loaded.Payload)
}

func TestDeleteExpiredLogDetailsBatchOnlyDeletesExpiredRows(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create(&LogDetail{RequestId: "req_expired", UserId: 1, CreatedAt: 10}).Error)
	require.NoError(t, LOG_DB.Create(&LogDetail{RequestId: "req_recent", UserId: 1, CreatedAt: 30}).Error)

	deleted, err := DeleteExpiredLogDetailsBatch(context.Background(), 20, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(1), deleted)

	remaining, err := CountExpiredLogDetails(context.Background(), 40)
	require.NoError(t, err)
	assert.Equal(t, int64(1), remaining)

	var recentCount int64
	require.NoError(t, LOG_DB.Model(&LogDetail{}).Where("request_id = ?", "req_recent").Count(&recentCount).Error)
	assert.Equal(t, int64(1), recentCount)
}

func TestReclaimLogDetailStorageShrinksSQLiteFile(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "log-detail-reclaim.db")
	testDB, err := gorm.Open(sqlite.Open(databasePath+"?_busy_timeout=30000"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := testDB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, testDB.AutoMigrate(&LogDetail{}))

	previousLogDB := LOG_DB
	previousLogDatabaseType := common.LogDatabaseType()
	LOG_DB = testDB
	common.SetLogDatabaseType(common.DatabaseTypeSQLite)
	t.Cleanup(func() {
		LOG_DB = previousLogDB
		common.SetLogDatabaseType(previousLogDatabaseType)
		_ = sqlDB.Close()
	})

	details := make([]LogDetail, 96)
	for index := range details {
		details[index] = LogDetail{
			RequestId:   fmt.Sprintf("req_reclaim_%03d", index),
			UserId:      1,
			CreatedAt:   int64(index + 1),
			RequestBody: LogDetailLargeText(strings.Repeat(fmt.Sprintf("%08d", index), 8192)),
		}
	}
	require.NoError(t, testDB.Session(&gorm.Session{SkipHooks: true}).CreateInBatches(details, 8).Error)
	require.NoError(t, testDB.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&LogDetail{}).Error)

	beforeReclaim, err := os.Stat(databasePath)
	require.NoError(t, err)
	require.Greater(t, beforeReclaim.Size(), int64(4<<20))
	require.NoError(t, ReclaimLogDetailStorage(context.Background()))
	afterReclaim, err := os.Stat(databasePath)
	require.NoError(t, err)
	assert.Less(t, afterReclaim.Size(), beforeReclaim.Size()/2)
}

func TestValidateMySQLTableMaintenanceResults(t *testing.T) {
	tests := []struct {
		name      string
		results   []mySQLTableMaintenanceResult
		wantError string
	}{
		{
			name: "rebuild note followed by success",
			results: []mySQLTableMaintenanceResult{
				{MessageType: "note", MessageText: "Table does not support optimize, doing recreate + analyze instead"},
				{MessageType: "status", MessageText: "OK"},
			},
		},
		{
			name: "table-level error",
			results: []mySQLTableMaintenanceResult{
				{MessageType: "Error", MessageText: "The table '#sql-temp' is full"},
				{MessageType: "status", MessageText: "Operation failed"},
			},
			wantError: "The table '#sql-temp' is full; Operation failed",
		},
		{
			name: "non-success status",
			results: []mySQLTableMaintenanceResult{
				{MessageType: "status", MessageText: "Operation failed"},
			},
			wantError: "Operation failed",
		},
		{
			name: "missing final status",
			results: []mySQLTableMaintenanceResult{
				{MessageType: "note", MessageText: "rebuild requested"},
			},
			wantError: "did not report status OK",
		},
		{
			name:      "empty result",
			wantError: "returned no result",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateMySQLTableMaintenanceResults(test.results)
			if test.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), test.wantError)
		})
	}
}

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
