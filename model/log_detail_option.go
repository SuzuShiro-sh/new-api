// 本文件把请求响应详情配置接入通用 Option 存储。
package model

import (
	"strconv"

	"github.com/QuantumNous/new-api/common"
)

func initLogDetailOptions() {
	common.OptionMap["LogDetailEnabled"] = strconv.FormatBool(common.LogDetailEnabled)
	common.OptionMap["LogDetailRetentionDays"] = strconv.Itoa(common.LogDetailRetentionDays)
	common.OptionMap["LogDetailMaxBodyKB"] = strconv.Itoa(common.LogDetailMaxBodyKB)
}

func updateLogDetailOption(key string, value string) (bool, error) {
	if err := validateLogDetailOption(key, value); err != nil {
		return true, err
	}
	switch key {
	case "LogDetailEnabled":
		parsed, _ := strconv.ParseBool(value)
		common.LogDetailEnabled = parsed
		return true, nil
	case "LogDetailRetentionDays":
		parsed, _ := strconv.Atoi(value)
		common.LogDetailRetentionDays = parsed
		return true, nil
	case "LogDetailMaxBodyKB":
		parsed, _ := strconv.Atoi(value)
		common.LogDetailMaxBodyKB = parsed
		return true, nil
	default:
		return false, nil
	}
}

// validateLogDetailOption 在写入数据库之前校验，避免失败的设置请求留下无效持久化值。
func validateLogDetailOption(key, value string) error {
	switch key {
	case "LogDetailEnabled":
		if _, err := strconv.ParseBool(value); err != nil {
			return common.NewMessage("Log detail logging must be a boolean")
		}
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
	}
	return nil
}
