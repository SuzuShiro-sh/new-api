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
	case "QuotaPerUnit":
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed <= 0 {
			return common.NewMessage("Quota per unit must be a finite positive number")
		}
	}
	return nil
}
