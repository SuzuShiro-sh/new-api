package model

import (
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

// LogDetailLargeText keeps request and response detail columns large enough for
// the configured capture limit while still letting each database use its native
// text type.
type LogDetailLargeText string

func (LogDetailLargeText) GormDataType() string {
	return "text"
}

func (LogDetailLargeText) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db != nil && db.Dialector.Name() == "mysql" {
		return "MEDIUMTEXT"
	}
	return "TEXT"
}

// LogDetail stores text-only request and response details for a relay request.
// Large media payloads and binary responses are intentionally omitted.
type LogDetail struct {
	Id               int                `json:"id" gorm:"primaryKey"`
	RequestId        string             `json:"request_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	UserId           int                `json:"user_id" gorm:"index;not null"`
	CreatedAt        int64              `json:"created_at" gorm:"bigint;index"`
	UpdatedAt        int64              `json:"updated_at" gorm:"bigint"`
	RequestModel     string             `json:"request_model" gorm:"type:varchar(191);index;default:''"`
	RequestPath      string             `json:"request_path" gorm:"type:varchar(512);default:''"`
	RequestMethod    string             `json:"request_method" gorm:"type:varchar(16);default:''"`
	RelayFormat      string             `json:"relay_format" gorm:"type:varchar(32);default:''"`
	IsStream         bool               `json:"is_stream" gorm:"index"`
	StatusCode       int                `json:"status_code" gorm:"default:0"`
	RequestBody      LogDetailLargeText `json:"request_body"`
	RequestParams    LogDetailLargeText `json:"request_params"`
	ResponseBody     LogDetailLargeText `json:"response_body"`
	RawResponseBody  LogDetailLargeText `json:"raw_response_body"`
	ErrorBody        LogDetailLargeText `json:"error_body"`
	ContentTruncated bool               `json:"content_truncated" gorm:"default:false"`
	ContentOmitted   bool               `json:"content_omitted" gorm:"default:false"`
	OmitReason       string             `json:"omit_reason" gorm:"type:varchar(255);default:''"`
}

func GetLogDetailByRequestId(requestId string) (*LogDetail, error) {
	if requestId == "" {
		return nil, errors.New("request_id is required")
	}
	var detail LogDetail
	if err := LOG_DB.Where("request_id = ?", requestId).First(&detail).Error; err != nil {
		return nil, err
	}
	return &detail, nil
}

func CheckLogDetailAccess(requestId string, userId int, isAdmin bool) error {
	if requestId == "" {
		return errors.New("request_id is required")
	}
	if isAdmin {
		return LOG_DB.Model(&Log{}).Where("request_id = ?", requestId).First(&Log{}).Error
	}
	if userId == 0 {
		return gorm.ErrRecordNotFound
	}
	return LOG_DB.Model(&Log{}).Where("request_id = ? AND user_id = ?", requestId, userId).First(&Log{}).Error
}

func DeleteLogDetailIfNoLog(requestId string) error {
	if requestId == "" {
		return nil
	}
	return LOG_DB.Transaction(func(tx *gorm.DB) error {
		var count int64
		if err := tx.Model(&Log{}).Where("request_id = ?", requestId).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return nil
		}
		return tx.Where("request_id = ?", requestId).Delete(&LogDetail{}).Error
	})
}
