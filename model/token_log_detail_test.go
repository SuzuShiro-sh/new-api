package model

import (
	"database/sql"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestBackfillTokenLogDetailEnabledDefaultsNullRowsToFalse 验证升级前令牌默认关闭详情采集.
func TestBackfillTokenLogDetailEnabledDefaultsNullRowsToFalse(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&Token{}))

	token := &Token{UserId: 1, Key: "legacy-detail-token", Name: "legacy"}
	require.NoError(t, testDB.Create(token).Error)
	require.NoError(t, testDB.Exec(
		"UPDATE tokens SET log_detail_enabled = NULL WHERE id = ?",
		token.Id,
	).Error)

	previousDB := DB
	DB = testDB
	t.Cleanup(func() {
		DB = previousDB
		sqlDB, dbErr := testDB.DB()
		if dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	require.NoError(t, backfillTokenLogDetailEnabled())
	var stored sql.NullBool
	require.NoError(t, testDB.Table("tokens").
		Select("log_detail_enabled").
		Where("id = ?", token.Id).
		Row().Scan(&stored))
	assert.True(t, stored.Valid)
	assert.False(t, stored.Bool)
}
