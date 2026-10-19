package model

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"slices"

	"gorm.io/gorm"
)

// 原始记录按块压缩存储，导出为串联 gzip 成员；解压后是逐行 JSON。
// 单块输入固定为 64 KiB，数据库驱动不需要持有整次请求的媒体正文。
const LogDetailArchiveChunkBytes = 64 * 1024

type LogDetailArchiveChunk struct {
	ID        int64                 `gorm:"primaryKey"`
	RequestID string                `gorm:"type:varchar(64);uniqueIndex:idx_detail_archive_order,priority:1;not null"`
	Ordinal   int                   `gorm:"uniqueIndex:idx_detail_archive_order,priority:2;not null"`
	Data      LogDetailPayloadBytes `gorm:"not null"`
}

func hasLogDetailArchiveTable(db *gorm.DB) (bool, error) {
	tables, err := db.Migrator().GetTables()
	return slices.Contains(tables, "log_detail_archive_chunks"), err
}

func saveLogDetailArchive(tx *gorm.DB, detail *LogDetail) error {
	detail.ArchiveBytes = 0
	if err := tx.Where("request_id = ?", detail.RequestId).Delete(&LogDetailArchiveChunk{}).Error; err != nil {
		return err
	}
	if detail.Archive == nil {
		return nil
	}
	buffer := make([]byte, LogDetailArchiveChunkBytes)
	var compressed bytes.Buffer
	compressor, err := gzip.NewWriterLevel(&compressed, gzip.BestSpeed)
	if err != nil {
		return err
	}
	defer compressor.Close()
	for ordinal := 0; ; ordinal++ {
		n, readErr := io.ReadFull(detail.Archive, buffer)
		if readErr != nil && readErr != io.EOF && readErr != io.ErrUnexpectedEOF {
			return fmt.Errorf("read log detail archive: %w", readErr)
		}
		if n == 0 {
			break
		}
		compressed.Reset()
		compressor.Reset(&compressed)
		if _, err = compressor.Write(buffer[:n]); err != nil {
			return err
		}
		if err = compressor.Close(); err != nil {
			return err
		}
		chunk := LogDetailArchiveChunk{RequestID: detail.RequestId, Ordinal: ordinal, Data: compressed.Bytes()}
		if err = tx.Create(&chunk).Error; err != nil {
			return err
		}
		detail.ArchiveBytes += int64(len(chunk.Data))
		if readErr != nil {
			break
		}
	}
	return tx.Model(&LogDetail{}).Where("request_id = ?", detail.RequestId).
		UpdateColumn("archive_bytes", detail.ArchiveBytes).Error
}

// WriteLogDetailArchive 逐块导出，内存占用不随归档总大小增长。
func WriteLogDetailArchive(ctx context.Context, requestID string, expectedBytes int64, destination io.Writer) error {
	// 单条 SELECT 保持同一读取快照；分条查询会在清理并发删除时导出半份归档。
	rows, err := LOG_DB.WithContext(ctx).Model(&LogDetailArchiveChunk{}).
		Where("request_id = ?", requestID).Order("ordinal").Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	var written int64
	ordinal := 0
	for rows.Next() {
		var chunk LogDetailArchiveChunk
		if err := LOG_DB.ScanRows(rows, &chunk); err != nil {
			return err
		}
		if chunk.Ordinal != ordinal {
			return io.ErrUnexpectedEOF
		}
		ordinal++
		n, writeErr := destination.Write(chunk.Data)
		written += int64(n)
		if writeErr != nil {
			return writeErr
		}
		if n != len(chunk.Data) {
			return io.ErrShortWrite
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if written != expectedBytes {
		return io.ErrUnexpectedEOF
	}
	return nil
}
