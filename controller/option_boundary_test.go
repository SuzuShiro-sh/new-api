// 本文件验证通用设置接口对额度换算配置的强边界约束。
package controller

import (
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
				"Quota per unit must be a finite positive number",
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
			name:    "zero quota per unit",
			body:    `{"key":"QuotaPerUnit","value":0}`,
			message: "Quota per unit must be a finite positive number",
		},
		{
			name:    "non finite quota per unit",
			body:    `{"key":"QuotaPerUnit","value":"NaN"}`,
			message: "Quota per unit must be a finite positive number",
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
				Success    bool   `json:"success"`
				Message    string `json:"message"`
				MessageKey string `json:"message_key"`
			}
			require.NoError(t, common.Unmarshal(response.Body.Bytes(), &payload))
			assert.False(t, payload.Success)
			assert.Equal(t, test.message, payload.Message)
			assert.NotEmpty(t, payload.MessageKey)
		})
	}
}
