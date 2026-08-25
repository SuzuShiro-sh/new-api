// 本文件集中校验通用设置接口中需要强边界约束的配置值。
package controller

import (
	"fmt"
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
			return fmt.Errorf("日志详情保留天数必须在 0 到 %d 之间", common.MaxLogDetailRetentionDays)
		}
	case "LogDetailMaxBodyKB":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < common.MinLogDetailBodyKB || parsed > common.MaxLogDetailBodyKB {
			return fmt.Errorf("日志详情单段上限必须在 %d KiB 到 %d KiB 之间", common.MinLogDetailBodyKB, common.MaxLogDetailBodyKB)
		}
	case "QuotaPerUnit":
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || math.IsNaN(parsed) || math.IsInf(parsed, 0) || parsed <= 0 {
			return fmt.Errorf("额度换算单位必须是大于 0 的有限数字")
		}
	}
	return nil
}
