// 本文件集中定义请求响应详情的运行时配置与边界。
package common

const (
	MinLogDetailBodyKB        = 16
	MaxLogDetailBodyKB        = 5 * 1024
	MaxLogDetailRetentionDays = 3650
)

var (
	LogDetailEnabled       = true
	LogDetailRetentionDays = 3
	LogDetailMaxBodyKB     = MaxLogDetailBodyKB
)
