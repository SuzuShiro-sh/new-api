// 本文件验证通用设置接口对日志详情和额度换算配置的强边界约束。
package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateOptionBoundaryRejectsInvalidQuotaPerUnit(t *testing.T) {
	for _, value := range []string{"0", "-1", "NaN", "+Inf", "not-a-number"} {
		t.Run(value, func(t *testing.T) {
			assert.EqualError(
				t,
				validateOptionBoundary("QuotaPerUnit", value),
				"额度换算单位必须是大于 0 的有限数字",
			)
		})
	}

	require.NoError(t, validateOptionBoundary("QuotaPerUnit", "0.5"))
}

func TestUpdateOptionRejectsInvalidOptionBoundaries(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		message string
	}{
		{
			name:    "negative retention",
			body:    `{"key":"LogDetailRetentionDays","value":-1}`,
			message: fmt.Sprintf("日志详情保留天数必须在 0 到 %d 之间", common.MaxLogDetailRetentionDays),
		},
		{
			name:    "body limit above maximum",
			body:    `{"key":"LogDetailMaxBodyKB","value":5121}`,
			message: fmt.Sprintf("日志详情单段上限必须在 %d KiB 到 %d KiB 之间", common.MinLogDetailBodyKB, common.MaxLogDetailBodyKB),
		},
		{
			name:    "zero quota per unit",
			body:    `{"key":"QuotaPerUnit","value":0}`,
			message: "额度换算单位必须是大于 0 的有限数字",
		},
		{
			name:    "non finite quota per unit",
			body:    `{"key":"QuotaPerUnit","value":"NaN"}`,
			message: "额度换算单位必须是大于 0 的有限数字",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			context, _ := gin.CreateTestContext(response)
			context.Request = httptest.NewRequest(
				http.MethodPut,
				"/api/option/",
				strings.NewReader(test.body),
			)

			UpdateOption(context)

			assert.Equal(t, http.StatusOK, response.Code)
			var payload struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			assert.False(t, payload.Success)
			assert.Equal(t, test.message, payload.Message)
		})
	}
}
