// 本文件负责在实际日志数据库中迁移请求响应详情表。
package model

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

func migrateSharedLogDetailDatabase() error {
	if os.Getenv("LOG_SQL_DSN") != "" {
		return nil
	}
	return migrateLogDetailDatabase(DB)
}

func migrateSQLLogDatabase(db *gorm.DB) error {
	if db == nil {
		return errors.New("log database is not initialized")
	}
	if err := db.AutoMigrate(&Log{}); err != nil {
		return err
	}
	return migrateLogDetailDatabase(db)
}

func migrateLogDetailDatabase(db *gorm.DB) error {
	if db == nil {
		return errors.New("log database is not initialized")
	}
	if err := db.AutoMigrate(&LogDetail{}); err != nil {
		return err
	}
	return migrateLogDetailLargeTextColumns(db)
}

func migrateLogDetailLargeTextColumns(db *gorm.DB) error {
	if db == nil || db.Dialector.Name() != "mysql" {
		return nil
	}
	tableName := "log_details"
	if !db.Migrator().HasTable(tableName) {
		return nil
	}
	for _, columnName := range []string{
		"request_body",
		"request_params",
		"response_body",
		"raw_response_body",
		"error_body",
	} {
		if !db.Migrator().HasColumn(&LogDetail{}, columnName) {
			continue
		}
		var columnType string
		if err := db.Raw(`SELECT COLUMN_TYPE FROM information_schema.columns
				WHERE table_schema = DATABASE() AND table_name = ? AND column_name = ?`,
			tableName, columnName).Scan(&columnType).Error; err != nil {
			common.SysLog(fmt.Sprintf("Warning: failed to query metadata for %s.%s: %v", tableName, columnName, err))
			continue
		}
		if strings.EqualFold(columnType, "mediumtext") || strings.EqualFold(columnType, "longtext") {
			continue
		}
		if err := db.Exec(fmt.Sprintf("ALTER TABLE %s MODIFY COLUMN %s MEDIUMTEXT", tableName, columnName)).Error; err != nil {
			return fmt.Errorf("failed to migrate %s.%s to MEDIUMTEXT: %w", tableName, columnName, err)
		}
	}
	return nil
}
