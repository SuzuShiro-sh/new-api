// 本文件验证无效额度换算配置不会污染运行时设置。
package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOptionValueRejectsInvalidQuotaPerUnit(t *testing.T) {
	for _, value := range []string{"0", "-1", "NaN", "+Inf", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			assert.EqualError(
				t,
				validateOptionValue("QuotaPerUnit", value),
				"QuotaPerUnit must be a finite positive number",
			)
		})
	}

	require.NoError(t, validateOptionValue("QuotaPerUnit", "0.5"))
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
		"QuotaPerUnit must be a finite positive number",
	)
	assert.Equal(t, 500000.0, common.QuotaPerUnit)
	common.OptionMapRWMutex.Lock()
	defer common.OptionMapRWMutex.Unlock()
	assert.Equal(t, "500000", common.OptionMap["QuotaPerUnit"])
}
