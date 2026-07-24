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

func TestUpdateOptionRejectsInvalidLogDetailLimits(t *testing.T) {
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
