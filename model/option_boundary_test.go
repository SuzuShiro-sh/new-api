// 本文件验证无效额度换算配置不会污染运行时设置。
package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestValidateOptionValueRejectsInvalidQuotaPerUnit(t *testing.T) {
	for _, value := range []string{"0", "-1", "NaN", "+Inf", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			assert.EqualError(
				t,
				validateOptionValue("QuotaPerUnit", value),
				"Quota per unit must be a finite positive number",
			)
		})
	}

	require.NoError(t, validateOptionValue("QuotaPerUnit", "0.5"))
}

func TestInvalidLogDetailOptionsDoNotPersist(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	previousDB := DB
	DB = db
	t.Cleanup(func() { DB = previousDB; require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.AutoMigrate(&Option{}))
	for _, test := range []struct{ key, valid, invalid string }{
		{"LogDetailEnabled", "true", "invalid"},
		{"LogDetailRetentionDays", "3", "-1"},
		{"LogDetailMaxBodyKB", "5120", "5121"},
	} {
		t.Run(test.key, func(t *testing.T) {
			require.NoError(t, db.Create(&Option{Key: test.key, Value: test.valid}).Error)
			for _, bulk := range []bool{false, true} {
				if bulk {
					err = UpdateOptionsBulk(map[string]string{test.key: test.invalid})
				} else {
					err = UpdateOption(test.key, test.invalid)
				}
				require.Error(t, err)
				var stored Option
				require.NoError(t, db.Where("key = ?", test.key).First(&stored).Error)
				assert.Equal(t, test.valid, stored.Value)
			}
		})
	}
}

func TestUpdateOptionMapRejectsInvalidQuotaPerUnitWithoutMutation(t *testing.T) {
	originalQuotaPerUnit := common.QuotaPerUnit
	common.OptionMapRWMutex.Lock()
	if common.OptionMap == nil {
		common.OptionMap = make(map[string]string)
	}
	originalMapValue, hadOriginalMapValue := common.OptionMap["QuotaPerUnit"]
	common.OptionMap["QuotaPerUnit"] = "500000"
	common.OptionMapRWMutex.Unlock()
	common.QuotaPerUnit = 500000

	t.Cleanup(func() {
		common.QuotaPerUnit = originalQuotaPerUnit
		common.OptionMapRWMutex.Lock()
		defer common.OptionMapRWMutex.Unlock()
		if hadOriginalMapValue {
			common.OptionMap["QuotaPerUnit"] = originalMapValue
		} else {
			delete(common.OptionMap, "QuotaPerUnit")
		}
	})

	require.EqualError(
		t,
		updateOptionMap("QuotaPerUnit", "0"),
		"Quota per unit must be a finite positive number",
	)
	assert.Equal(t, 500000.0, common.QuotaPerUnit)
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	assert.Equal(t, "500000", common.OptionMap["QuotaPerUnit"])
}
