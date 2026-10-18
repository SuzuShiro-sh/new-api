// 本文件验证请求响应详情的压缩、授权、清理和回收契约。
package model

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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

// openLogDetailMatrixDB 为原生表名、事务与空间回收测试建立独立数据库或 schema。
func openLogDetailMatrixDB(t *testing.T, dialect string) *gorm.DB {
	t.Helper()
	dsn := "local"
	if dialect != "sqlite" {
		dsn = os.Getenv("TEST_" + strings.ToUpper(dialect) + "_DSN")
		if dsn == "" {
			t.Skip("test database DSN is not configured")
		}
	} else {
		previousPath := common.SQLitePath
		common.SQLitePath = filepath.Join(t.TempDir(), "details.db")
		t.Cleanup(func() { common.SQLitePath = previousPath })
	}
	t.Setenv("LOG_DETAIL_TEST_DSN", dsn)
	db, _, err := chooseDB("LOG_DETAIL_TEST_DSN", true)
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { assert.NoError(t, sqlDB.Close()) })
	name := fmt.Sprintf("prompt_detail_test_%d", time.Now().UnixNano())
	switch dialect {
	case "mysql":
		require.NoError(t, db.Exec("CREATE DATABASE ?", clause.Table{Name: name}).Error)
		t.Cleanup(func() { assert.NoError(t, db.Exec("DROP DATABASE ?", clause.Table{Name: name}).Error) })
		// USE 不支持 MySQL 预处理协议；此处只切换测试连接的隔离数据库。
		_, err = sqlDB.ExecContext(context.Background(), "USE `"+name+"`")
		require.NoError(t, err)
	case "postgres":
		require.NoError(t, db.Exec("CREATE SCHEMA ?", clause.Table{Name: name}).Error)
		t.Cleanup(func() { assert.NoError(t, db.Exec("DROP SCHEMA ? CASCADE", clause.Table{Name: name}).Error) })
		require.NoError(t, db.Exec("SET search_path TO ?", clause.Table{Name: name}).Error)
	}
	return db
}

func TestLogDetailDatabaseMatrix(t *testing.T) {
	for _, dialect := range []string{"sqlite", "mysql", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			for _, upgrade := range []bool{false, true} {
				t.Run(fmt.Sprintf("upgrade=%v", upgrade), func(t *testing.T) {
					db := openLogDetailMatrixDB(t, dialect)
					previousDB, previousLogDB := DB, LOG_DB
					previousMainType, previousLogType := common.MainDatabaseType(), common.LogDatabaseType()
					DB, LOG_DB = db, db
					common.SetDatabaseTypes(common.DatabaseType(dialect), common.DatabaseType(dialect))
					t.Cleanup(func() {
						DB, LOG_DB = previousDB, previousLogDB
						common.SetDatabaseTypes(previousMainType, previousLogType)
					})
					versionSQL := "SELECT version()"
					if dialect == "sqlite" {
						versionSQL = "SELECT sqlite_version()"
					}
					var version string
					require.NoError(t, db.Raw(versionSQL).Scan(&version).Error)
					t.Logf("database version: %s", version)

					if upgrade {
						// 上游已有普通日志与令牌，没有详情字段；另保留历史明文详情验证旧数据读取。
						require.NoError(t, db.AutoMigrate(&Log{}, &Token{}))
						require.NoError(t, db.Migrator().DropColumn(&Token{}, "LogDetailEnabled"))
						require.NoError(t, db.Table("tokens").Create(map[string]any{
							"user_id": 7, "key": "legacy-key", "name": "legacy", "allow_user_quota_query": true,
						}).Error)
						require.NoError(t, db.Create(&Log{RequestId: "legacy", UserId: 7, CreatedAt: 10, Type: LogTypeConsume}).Error)
						require.NoError(t, db.AutoMigrate(&LogDetail{}))
						require.NoError(t, db.Session(&gorm.Session{SkipHooks: true}).Create(&LogDetail{
							RequestId: "legacy", UserId: 7, CreatedAt: 10, RequestBody: "旧请求", ResponseBody: "旧响应",
						}).Error)
					}
					for range 2 {
						require.NoError(t, db.AutoMigrate(&Token{}, &Option{}))
						require.NoError(t, backfillTokenLogDetailEnabled())
						require.NoError(t, migrateSQLLogDatabase(db))
					}
					if upgrade {
						legacy, err := GetAccessibleLogDetailByRequestId("legacy", 7, false)
						require.NoError(t, err)
						assert.Equal(t, LogDetailLargeText("旧请求"), legacy.RequestBody)
						var token Token
						require.NoError(t, db.Where("name = ?", "legacy").First(&token).Error)
						assert.False(t, token.LogDetailEnabled)
						assert.True(t, token.AllowUserQuotaQuery)
						token.LogDetailEnabled = true
						require.NoError(t, token.Update())
						require.NoError(t, db.First(&token, token.Id).Error)
						assert.True(t, token.LogDetailEnabled)
						assert.True(t, token.AllowUserQuotaQuery)
					}
					// 主库保持独立，后续查询、写入及清理必须只访问当前日志库。
					DB = previousDB
					for _, requestID := range []string{"old-detail", "recent-detail"} {
						createdAt := int64(30)
						if requestID == "old-detail" {
							createdAt = 10
						}
						require.NoError(t, db.Create(&Log{RequestId: requestID, UserId: 7, CreatedAt: createdAt, Type: LogTypeConsume}).Error)
						for _, body := range []string{"initial", strings.Repeat("中文正文", 20000)} {
							saved, err := SaveLogDetailIfLogExists(context.Background(), &LogDetail{
								RequestId: requestID, UserId: 7, CreatedAt: createdAt, RequestBody: LogDetailLargeText(body),
							})
							require.NoError(t, err)
							require.True(t, saved)
							loaded, err := GetAccessibleLogDetailByRequestId(requestID, 7, false)
							require.NoError(t, err)
							assert.Equal(t, body, string(loaded.RequestBody))
						}
						_, err := GetAccessibleLogDetailByRequestId(requestID, 8, false)
						assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
					}
					deleted, err := DeleteOldLogBatch(context.Background(), 20, 1)
					require.NoError(t, err)
					assert.EqualValues(t, 1, deleted)
					_, err = GetAccessibleLogDetailByRequestId("recent-detail", 7, false)
					require.NoError(t, err)
					cleared, err := ClearAllLogDetailsAndReclaim(context.Background())
					require.NoError(t, err)
					assert.True(t, cleared.SpaceReclaimed)
					var logs int64
					require.NoError(t, db.Model(&Log{}).Count(&logs).Error)
					assert.GreaterOrEqual(t, logs, int64(1))
				})
			}
		})
	}
}

func TestDeleteOldLogBatchWithoutDetailsRespectsBatchLimit(t *testing.T) {
	db := openLogDetailMatrixDB(t, "sqlite")
	require.NoError(t, db.AutoMigrate(&Log{}))
	previousLogDB := LOG_DB
	LOG_DB = db
	t.Cleanup(func() { LOG_DB = previousLogDB })
	require.NoError(t, db.Create(&[]Log{
		{RequestId: "old-1", CreatedAt: 1},
		{RequestId: "old-2", CreatedAt: 2},
		{RequestId: "recent", CreatedAt: 30},
	}).Error)
	deleted, err := DeleteOldLogBatch(context.Background(), 20, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	var remaining int64
	require.NoError(t, db.Model(&Log{}).Count(&remaining).Error)
	assert.EqualValues(t, 2, remaining)
}

func TestDeleteOldLogBatchRetainsDetailsForRemainingRetryLog(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create(&[]Log{
		{RequestId: "shared-retry", UserId: 7, CreatedAt: 10, Type: LogTypeError},
		{RequestId: "shared-retry", UserId: 7, CreatedAt: 11, Type: LogTypeConsume},
	}).Error)
	require.NoError(t, LOG_DB.Create(&LogDetail{RequestId: "shared-retry", UserId: 7, CreatedAt: 10, RequestBody: "preserve until last log"}).Error)
	deleted, err := DeleteOldLogBatch(context.Background(), 20, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	remaining, err := GetAccessibleLogDetailByRequestId("shared-retry", 7, false)
	require.NoError(t, err)
	assert.Equal(t, LogDetailLargeText("preserve until last log"), remaining.RequestBody)
	deleted, err = DeleteOldLogBatch(context.Background(), 20, 1)
	require.NoError(t, err)
	assert.EqualValues(t, 1, deleted)
	_, err = GetLogDetailByRequestId("shared-retry")
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)
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

func TestClearAllLogDetailsAndReclaimShrinksSQLiteFileAndPreservesLogs(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "log-detail-reclaim.db")
	testDB, err := gorm.Open(sqlite.Open(databasePath+"?_busy_timeout=30000"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := testDB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, testDB.AutoMigrate(&Log{}, &LogDetail{}))

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
	require.NoError(t, testDB.Create(&Log{
		UserId:    1,
		CreatedAt: 1,
		Type:      LogTypeConsume,
		RequestId: "req_reclaim_000",
	}).Error)

	beforeReclaim, err := os.Stat(databasePath)
	require.NoError(t, err)
	require.Greater(t, beforeReclaim.Size(), int64(4<<20))

	total, err := CountLogDetails(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(len(details)), total)
	clearResult, err := ClearAllLogDetailsAndReclaim(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(len(details)), clearResult.DeletedCount)
	assert.True(t, clearResult.SpaceReclaimed)

	remaining, err := CountLogDetails(context.Background())
	require.NoError(t, err)
	assert.Zero(t, remaining)
	var logCount int64
	require.NoError(t, testDB.Model(&Log{}).Count(&logCount).Error)
	assert.Equal(t, int64(1), logCount)

	afterReclaim, err := os.Stat(databasePath)
	require.NoError(t, err)
	assert.Less(t, afterReclaim.Size(), beforeReclaim.Size()/2)
}

func TestGetAccessibleLogDetailByRequestIdEnforcesOwnership(t *testing.T) {
	truncateTables(t)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    7,
		CreatedAt: 10,
		Type:      LogTypeConsume,
		RequestId: "req_accessible",
	}).Error)
	require.NoError(t, LOG_DB.Create(&LogDetail{
		RequestId:   "req_accessible",
		UserId:      7,
		CreatedAt:   10,
		RequestBody: "owned",
	}).Error)

	owned, err := GetAccessibleLogDetailByRequestId("req_accessible", 7, false)
	require.NoError(t, err)
	assert.Equal(t, "owned", string(owned.RequestBody))

	_, err = GetAccessibleLogDetailByRequestId("req_accessible", 8, false)
	assert.ErrorIs(t, err, gorm.ErrRecordNotFound)

	admin, err := GetAccessibleLogDetailByRequestId("req_accessible", 0, true)
	require.NoError(t, err)
	assert.Equal(t, "req_accessible", admin.RequestId)
}

func TestLogDetailStoreUnavailableDoesNotQueryClickHouse(t *testing.T) {
	originalLogType := common.LogDatabaseType()
	t.Cleanup(func() { common.SetLogDatabaseType(originalLogType) })
	common.SetLogDatabaseType(common.DatabaseTypeClickHouse)

	_, err := GetLogDetailByRequestId("req_unavailable")
	assert.ErrorIs(t, err, ErrLogDetailStoreUnavailable)
	_, err = CountLogDetails(context.Background())
	assert.ErrorIs(t, err, ErrLogDetailStoreUnavailable)
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
