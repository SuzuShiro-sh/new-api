// 本文件定义请求响应详情的压缩存储、授权查询与原子写入。
package model

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"gorm.io/gorm/schema"
)

const (
	logDetailPayloadVersion         = 1
	maxLogDetailPayloadDecodedBytes = 4*common.MaxLogDetailBodyKB*1024 + 1<<20
)

var ErrLogDetailStoreUnavailable = errors.New("log detail store is unavailable")

// LogDetailLargeText 为历史详情列提供跨数据库的大文本类型兼容.
type LogDetailLargeText string

// GormDataType 返回历史详情列的通用 GORM 类型.
func (LogDetailLargeText) GormDataType() string {
	return "text"
}

// GormDBDataType 按数据库类型选择历史详情列的数据类型.
func (LogDetailLargeText) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db != nil && db.Dialector.Name() == "mysql" {
		return "MEDIUMTEXT"
	}
	return "TEXT"
}

// LogDetailPayloadBytes 为压缩详情负载提供跨数据库的二进制类型.
type LogDetailPayloadBytes []byte

// GormDataType 返回压缩详情负载的通用 GORM 类型.
func (LogDetailPayloadBytes) GormDataType() string {
	return "bytes"
}

// GormDBDataType 按数据库类型选择可容纳压缩详情的二进制类型.
func (LogDetailPayloadBytes) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	if db == nil {
		return "BLOB"
	}
	switch db.Dialector.Name() {
	case "mysql":
		return "LONGBLOB"
	case "postgres":
		return "BYTEA"
	default:
		return "BLOB"
	}
}

type logDetailPayload struct {
	RequestBody     string `json:"request_body,omitempty"`
	RequestParams   string `json:"request_params,omitempty"`
	ResponseBody    string `json:"response_body,omitempty"`
	RawResponseBody string `json:"raw_response_body,omitempty"`
	ErrorBody       string `json:"error_body,omitempty"`
}

// LogDetail 保存一次中继请求的文本详情. 新记录写入压缩负载, 历史文本列仅用于兼容旧数据.
type LogDetail struct {
	Id               int                   `json:"id" gorm:"primaryKey"`
	RequestId        string                `json:"request_id" gorm:"type:varchar(64);uniqueIndex;not null"`
	UserId           int                   `json:"user_id" gorm:"index;not null"`
	CreatedAt        int64                 `json:"created_at" gorm:"bigint;index"`
	UpdatedAt        int64                 `json:"updated_at" gorm:"bigint"`
	RequestModel     string                `json:"request_model" gorm:"type:varchar(191);index;default:''"`
	RequestPath      string                `json:"request_path" gorm:"type:varchar(512);default:''"`
	RequestMethod    string                `json:"request_method" gorm:"type:varchar(16);default:''"`
	RelayFormat      string                `json:"relay_format" gorm:"type:varchar(32);default:''"`
	IsStream         bool                  `json:"is_stream" gorm:"index"`
	StatusCode       int                   `json:"status_code" gorm:"default:0"`
	Payload          LogDetailPayloadBytes `json:"-"`
	PayloadVersion   int                   `json:"-" gorm:"default:0"`
	StoredBytes      int                   `json:"stored_bytes" gorm:"default:0"`
	RequestBody      LogDetailLargeText    `json:"request_body"`
	RequestParams    LogDetailLargeText    `json:"request_params"`
	ResponseBody     LogDetailLargeText    `json:"response_body"`
	RawResponseBody  LogDetailLargeText    `json:"raw_response_body"`
	ErrorBody        LogDetailLargeText    `json:"error_body"`
	ContentTruncated bool                  `json:"content_truncated" gorm:"default:false"`
	ContentOmitted   bool                  `json:"content_omitted" gorm:"default:false"`
	OmitReason       string                `json:"omit_reason" gorm:"type:varchar(255);default:''"`
}

// IsLogDetailStoreAvailable 返回当前日志库是否支持详情持久化。
func IsLogDetailStoreAvailable() bool {
	return LOG_DB != nil && !common.UsingLogDatabase(common.DatabaseTypeClickHouse)
}

// BeforeSave 将详情正文压缩到单个二进制负载并清空历史大文本列.
func (detail *LogDetail) BeforeSave(_ *gorm.DB) error {
	if detail == nil {
		return nil
	}
	payload := logDetailPayload{
		RequestBody:     string(detail.RequestBody),
		RequestParams:   string(detail.RequestParams),
		ResponseBody:    string(detail.ResponseBody),
		RawResponseBody: string(detail.RawResponseBody),
		ErrorBody:       string(detail.ErrorBody),
	}
	data, err := common.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal log detail payload: %w", err)
	}
	if len(data) > maxLogDetailPayloadDecodedBytes {
		return fmt.Errorf("log detail payload exceeds %d bytes", maxLogDetailPayloadDecodedBytes)
	}

	var compressed bytes.Buffer
	writer, err := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
	if err != nil {
		return fmt.Errorf("failed to create log detail compressor: %w", err)
	}
	if _, err = writer.Write(data); err != nil {
		_ = writer.Close()
		return fmt.Errorf("failed to compress log detail payload: %w", err)
	}
	if err = writer.Close(); err != nil {
		return fmt.Errorf("failed to finish log detail compression: %w", err)
	}

	detail.Payload = compressed.Bytes()
	detail.PayloadVersion = logDetailPayloadVersion
	detail.StoredBytes = len(detail.Payload)
	detail.RequestBody = ""
	detail.RequestParams = ""
	detail.ResponseBody = ""
	detail.RawResponseBody = ""
	detail.ErrorBody = ""
	return nil
}

// AfterFind 解压新格式负载, 并保持历史 API 字段不变.
func (detail *LogDetail) AfterFind(_ *gorm.DB) error {
	if detail == nil || detail.PayloadVersion == 0 || len(detail.Payload) == 0 {
		return nil
	}
	if detail.PayloadVersion != logDetailPayloadVersion {
		return fmt.Errorf("unsupported log detail payload version: %d", detail.PayloadVersion)
	}

	reader, err := gzip.NewReader(bytes.NewReader(detail.Payload))
	if err != nil {
		return fmt.Errorf("failed to open log detail payload: %w", err)
	}
	data, readErr := io.ReadAll(io.LimitReader(reader, maxLogDetailPayloadDecodedBytes+1))
	closeErr := reader.Close()
	if readErr != nil {
		return fmt.Errorf("failed to read log detail payload: %w", readErr)
	}
	if closeErr != nil {
		return fmt.Errorf("failed to close log detail payload: %w", closeErr)
	}
	if len(data) > maxLogDetailPayloadDecodedBytes {
		return fmt.Errorf("log detail payload exceeds %d decoded bytes", maxLogDetailPayloadDecodedBytes)
	}

	var payload logDetailPayload
	if err = common.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("failed to unmarshal log detail payload: %w", err)
	}
	detail.RequestBody = LogDetailLargeText(payload.RequestBody)
	detail.RequestParams = LogDetailLargeText(payload.RequestParams)
	detail.ResponseBody = LogDetailLargeText(payload.ResponseBody)
	detail.RawResponseBody = LogDetailLargeText(payload.RawResponseBody)
	detail.ErrorBody = LogDetailLargeText(payload.ErrorBody)
	return nil
}

// GetLogDetailByRequestId 按请求 ID 查询并解压详情.
func GetLogDetailByRequestId(requestId string) (*LogDetail, error) {
	if requestId == "" {
		return nil, errors.New("request_id is required")
	}
	if !IsLogDetailStoreAvailable() {
		return nil, ErrLogDetailStoreUnavailable
	}
	var detail LogDetail
	if err := LOG_DB.Where("request_id = ?", requestId).First(&detail).Error; err != nil {
		return nil, err
	}
	return &detail, nil
}

// GetAccessibleLogDetailByRequestId 只返回当前用户可访问且仍有关联普通日志的详情。
func GetAccessibleLogDetailByRequestId(requestId string, userId int, isAdmin bool) (*LogDetail, error) {
	if requestId == "" {
		return nil, errors.New("request_id is required")
	}
	if !IsLogDetailStoreAvailable() {
		return nil, ErrLogDetailStoreUnavailable
	}
	if !isAdmin && userId == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	query := LOG_DB.Model(&LogDetail{}).
		Joins("JOIN logs ON logs.request_id = log_details.request_id").
		Where("log_details.request_id = ?", requestId)
	if !isAdmin {
		query = query.Where("logs.user_id = ?", userId)
	}
	var detail LogDetail
	if err := query.First(&detail).Error; err != nil {
		return nil, err
	}
	return &detail, nil
}

// SaveLogDetailIfLogExists 仅在普通日志已落库时原子写入对应详情.
func SaveLogDetailIfLogExists(ctx context.Context, detail *LogDetail) (bool, error) {
	if detail == nil || detail.RequestId == "" {
		return false, errors.New("request_id is required")
	}
	if !IsLogDetailStoreAvailable() {
		return false, ErrLogDetailStoreUnavailable
	}

	saved := false
	err := LOG_DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var log Log
		if err := lockForUpdate(tx).Select("id").Where("request_id = ?", detail.RequestId).Take(&log).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil
			}
			return err
		}

		updates := []string{
			"user_id", "created_at", "updated_at", "request_model", "request_path",
			"request_method", "relay_format", "is_stream", "status_code", "payload",
			"payload_version", "stored_bytes", "request_body", "request_params",
			"response_body", "raw_response_body", "error_body", "content_truncated",
			"content_omitted", "omit_reason",
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "request_id"}},
			DoUpdates: clause.AssignmentColumns(updates),
		}).Create(detail).Error; err != nil {
			return err
		}
		saved = true
		return nil
	})
	return saved, err
}
