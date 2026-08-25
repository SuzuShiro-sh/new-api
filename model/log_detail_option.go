// 本文件把请求响应详情配置接入通用 Option 存储。
package model

import (
	"fmt"
	"strconv"

	"github.com/QuantumNous/new-api/common"
)

func initLogDetailOptions() {
	common.OptionMap["LogDetailEnabled"] = strconv.FormatBool(common.LogDetailEnabled)
	common.OptionMap["LogDetailRetentionDays"] = strconv.Itoa(common.LogDetailRetentionDays)
	common.OptionMap["LogDetailMaxBodyKB"] = strconv.Itoa(common.LogDetailMaxBodyKB)
}

func updateLogDetailOption(key string, value string) (bool, error) {
	switch key {
	case "LogDetailEnabled":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return true, fmt.Errorf("invalid log detail enabled value: %s", value)
		}
		common.LogDetailEnabled = parsed
		return true, nil
	case "LogDetailRetentionDays":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < 0 || parsed > common.MaxLogDetailRetentionDays {
			return true, fmt.Errorf("invalid log detail retention days: %s", value)
		}
		common.LogDetailRetentionDays = parsed
		return true, nil
	case "LogDetailMaxBodyKB":
		parsed, err := strconv.Atoi(value)
		if err != nil || parsed < common.MinLogDetailBodyKB || parsed > common.MaxLogDetailBodyKB {
			return true, fmt.Errorf("invalid log detail body limit: %s", value)
		}
		common.LogDetailMaxBodyKB = parsed
		return true, nil
	default:
		return false, nil
	}
}
