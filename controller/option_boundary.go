// 本文件集中校验通用设置接口中需要强边界约束的配置值。
package controller

import (
	"math"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

func validateOptionBoundary(key string, value string) error {
	switch key {
	case "LogDetailRetentionDays":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > common.MaxLogDetailRetentionDays {
			return common.NewMessage("Log detail retention days must be between {{min}} and {{max}}", map[string]any{"min": 0, "max": common.MaxLogDetailRetentionDays})
		}
	case "LogDetailMaxBodyKB":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < common.MinLogDetailBodyKB || parsed > common.MaxLogDetailBodyKB {
			return common.NewMessage("Log detail content limit must be between {{min}} and {{max}} KiB", map[string]any{"min": common.MinLogDetailBodyKB, "max": common.MaxLogDetailBodyKB})
		}
	case "QuotaPerUnit":
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed <= 0 {
			return common.NewMessage("Quota per unit must be a finite positive number")
		}
	}
	return nil
}
