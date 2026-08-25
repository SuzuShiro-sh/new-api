// 本文件负责令牌日志详情开关的升级数据初始化。
package model

func backfillTokenLogDetailEnabled() error {
	if !DB.Migrator().HasTable(&Token{}) || !DB.Migrator().HasColumn(&Token{}, "log_detail_enabled") {
		return nil
	}
	return DB.Unscoped().Model(&Token{}).
		Where("log_detail_enabled IS NULL").
		Update("log_detail_enabled", false).Error
}
