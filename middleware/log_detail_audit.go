// 本文件登记请求响应详情维护接口的审计事件。
package middleware

func init() {
	auditRouteActions["POST /api/system-task/log-detail-cleanup"] = "log.detail_cleanup_start"
	auditRouteActions["POST /api/system-task/log-detail-clear-all"] = "log.detail_clear_all_start"
}
