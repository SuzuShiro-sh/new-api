package service

import (
	"bufio"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	logDetailTextLimitBytes      = 5 << 20
	logDetailContextKey          = "new_api_log_detail_capture"
	logDetailRawCaptureMarkerKey = "new_api_log_detail_raw_capture"
)

var longBase64Pattern = regexp.MustCompile(`(?i)"data:(image|audio|video|application)(?:/|\\/)[^"]{128,}"?|"[A-Za-z0-9+/_=\\-]{1000,}"?`)

type logDetailCapture struct {
	requestId    string
	responseBody *limitedTextBuffer
	mu           sync.Mutex
}

type limitedTextBuffer struct {
	buf       strings.Builder
	limit     int
	seen      int
	truncated bool
	omitted   bool
	reason    string
}

func newLimitedTextBuffer() *limitedTextBuffer {
	return &limitedTextBuffer{limit: logDetailTextLimitBytes}
}

func (b *limitedTextBuffer) writeBytes(data []byte) {
	if b == nil || len(data) == 0 || b.omitted {
		return
	}
	b.seen += len(data)
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.truncated = true
		return
	}
	if len(data) > remaining {
		data = data[:remaining]
		b.truncated = true
	}
	b.buf.Write(data)
}

func (b *limitedTextBuffer) writeString(data string) {
	b.writeBytes(common.StringToByteSlice(data))
}

func (b *limitedTextBuffer) omit(reason string) {
	if b == nil || b.omitted {
		return
	}
	b.omitted = true
	b.reason = truncatePlainText(reason, 255)
	b.buf.Reset()
}

func (b *limitedTextBuffer) value() logDetailText {
	if b == nil {
		return logDetailText{}
	}
	return logDetailText{
		Text:      b.buf.String(),
		Original:  b.seen,
		Truncated: b.truncated,
		Omitted:   b.omitted,
		Reason:    b.reason,
	}
}

type logDetailText struct {
	Text      string
	Original  int
	Truncated bool
	Omitted   bool
	Reason    string
}

type logDetailMeta struct {
	RequestID               string   `json:"request_id,omitempty"`
	UpstreamRequestID       string   `json:"upstream_request_id,omitempty"`
	UserID                  int      `json:"user_id,omitempty"`
	TokenID                 int      `json:"token_id,omitempty"`
	ChannelID               int      `json:"channel_id,omitempty"`
	ChannelType             int      `json:"channel_type,omitempty"`
	ChannelName             string   `json:"channel_name,omitempty"`
	Model                   string   `json:"model,omitempty"`
	Method                  string   `json:"method,omitempty"`
	Path                    string   `json:"path,omitempty"`
	RelayMode               int      `json:"relay_mode,omitempty"`
	RelayFormat             string   `json:"relay_format,omitempty"`
	FinalRequestRelayFormat string   `json:"final_request_relay_format,omitempty"`
	RequestConversion       []string `json:"request_conversion,omitempty"`
	IsStream                bool     `json:"is_stream"`
	StatusCode              int      `json:"status_code,omitempty"`
	RequestContentType      string   `json:"request_content_type,omitempty"`
	ResponseContentType     string   `json:"response_content_type,omitempty"`
	RequestBodyBytes        int      `json:"request_body_bytes,omitempty"`
	RequestBodySavedBytes   int      `json:"request_body_saved_bytes,omitempty"`
	RequestBodyTruncated    bool     `json:"request_body_truncated,omitempty"`
	RequestBodyOmitted      bool     `json:"request_body_omitted,omitempty"`
	RequestBodyOmitReason   string   `json:"request_body_omit_reason,omitempty"`
	ResponseBodyBytes       int      `json:"response_body_bytes,omitempty"`
	ResponseBodySavedBytes  int      `json:"response_body_saved_bytes,omitempty"`
	ResponseBodyTruncated   bool     `json:"response_body_truncated,omitempty"`
	ResponseBodyOmitted     bool     `json:"response_body_omitted,omitempty"`
	ResponseBodyOmitReason  string   `json:"response_body_omit_reason,omitempty"`
	RawBodyBytes            int      `json:"raw_body_bytes,omitempty"`
	RawBodySavedBytes       int      `json:"raw_body_saved_bytes,omitempty"`
	RawBodyTruncated        bool     `json:"raw_body_truncated,omitempty"`
	RawBodyOmitted          bool     `json:"raw_body_omitted,omitempty"`
	RawBodyOmitReason       string   `json:"raw_body_omit_reason,omitempty"`
	ErrorBodyBytes          int      `json:"error_body_bytes,omitempty"`
	ErrorBodySavedBytes     int      `json:"error_body_saved_bytes,omitempty"`
	ErrorBodyTruncated      bool     `json:"error_body_truncated,omitempty"`
	ContentLimitBytes       int      `json:"content_limit_bytes"`
}

type responseBodyCapture struct {
	io.ReadCloser
	requestId   string
	contentType string
	buf         *limitedTextBuffer
	once        sync.Once
}

type LogDetailResponseWriter struct {
	gin.ResponseWriter
	requestId string
	buf       *limitedTextBuffer
	mu        *sync.Mutex
}

func CaptureRelayRequestDetail(c *gin.Context, info *relaycommon.RelayInfo) {
	if c == nil || info == nil {
		return
	}
	if !common.LogConsumeEnabled {
		return
	}
	requestId := requestIdFromContext(c, info)
	if requestId == "" {
		return
	}
	capture := &logDetailCapture{
		requestId:    requestId,
		responseBody: newLimitedTextBuffer(),
	}
	c.Set(logDetailContextKey, capture)
	wrappedWriter := &LogDetailResponseWriter{
		ResponseWriter: c.Writer,
		requestId:      requestId,
		buf:            capture.responseBody,
		mu:             &capture.mu,
	}
	c.Writer = wrappedWriter

	requestText := extractRequestDetailText(c)
	meta := buildLogDetailMeta(c, info, 0)
	applyTextMeta(&meta, "request", requestText)
	metaJSON := marshalLogDetailMeta(meta)

	now := common.GetTimestamp()
	detail := model.LogDetail{
		RequestId:        requestId,
		UserId:           info.UserId,
		CreatedAt:        now,
		UpdatedAt:        now,
		RequestModel:     info.OriginModelName,
		RequestPath:      requestPath(c),
		RequestMethod:    requestMethod(c),
		RelayFormat:      string(info.RelayFormat),
		IsStream:         info.IsStream,
		RequestBody:      model.LogDetailLargeText(requestText.Text),
		RequestParams:    model.LogDetailLargeText(metaJSON),
		ContentTruncated: requestText.Truncated,
		ContentOmitted:   requestText.Omitted,
		OmitReason:       truncatePlainText(requestText.Reason, 255),
	}
	if info.RelayFormat == types.RelayFormatOpenAIRealtime {
		detail.ContentOmitted = true
		detail.OmitReason = "websocket realtime frames are not captured"
	}
	upsertLogDetail(c, detail, []string{
		"user_id", "updated_at", "request_model", "request_path", "request_method",
		"relay_format", "is_stream", "request_body", "request_params",
		"content_truncated", "content_omitted", "omit_reason",
	})
}

func WrapLogDetailResponse(c *gin.Context, resp *http.Response, info *relaycommon.RelayInfo) *http.Response {
	if c == nil || resp == nil || resp.Body == nil {
		return resp
	}
	if !isLogDetailCaptureActive(c) {
		return resp
	}
	requestId := c.GetString(common.RequestIdKey)
	if requestId == "" && info != nil {
		requestId = info.RequestId
	}
	if requestId == "" {
		return resp
	}
	contentType := resp.Header.Get("Content-Type")
	capture := &responseBodyCapture{
		ReadCloser:  resp.Body,
		requestId:   requestId,
		contentType: contentType,
		buf:         newLimitedTextBuffer(),
	}
	c.Set(logDetailRawCaptureMarkerKey, true)
	if !isTextContentType(contentType) {
		capture.buf.omit("binary response content-type " + contentType)
	}
	resp.Body = capture
	UpdateLogDetailMeta(c, info, resp.StatusCode, contentType)
	return resp
}

func CaptureLogDetailBytesResponse(c *gin.Context, src *http.Response, data []byte) {
	if c == nil || len(data) == 0 {
		return
	}
	if !isLogDetailCaptureActive(c) {
		return
	}
	requestId := c.GetString(common.RequestIdKey)
	if requestId == "" {
		return
	}
	contentType := ""
	statusCode := 0
	if src != nil {
		contentType = src.Header.Get("Content-Type")
		statusCode = src.StatusCode
	}
	text := logDetailText{Original: len(data)}
	if isTextContentType(contentType) {
		text = sanitizeTextBody(string(data), contentType)
		text.Original = len(data)
	} else {
		text.Omitted = true
		text.Reason = "binary response content-type " + contentType
	}
	displayText := displayResponseDetailText(text, contentType, isEventStreamContentType(contentType))
	meta := buildLogDetailMeta(c, nil, statusCode)
	meta.ResponseContentType = contentType
	applyTextMeta(&meta, "raw", text)
	applyTextMeta(&meta, "response", displayText)
	fields := map[string]interface{}{
		"status_code":       statusCode,
		"response_body":     displayText.Text,
		"request_params":    marshalLogDetailMeta(meta),
		"content_truncated": gorm.Expr("content_truncated OR ?", text.Truncated || displayText.Truncated),
		"content_omitted":   gorm.Expr("content_omitted OR ?", text.Omitted || displayText.Omitted),
		"omit_reason":       mergedOmitReasonExpr(firstNonEmpty(text.Reason, displayText.Reason)),
		"updated_at":        common.GetTimestamp(),
	}
	if !hasUpstreamRawCapture(c, src) {
		fields["raw_response_body"] = text.Text
	}
	updateLogDetailFields(requestId, fields)
}

func hasUpstreamRawCapture(c *gin.Context, src *http.Response) bool {
	if c != nil && c.GetBool(logDetailRawCaptureMarkerKey) {
		return true
	}
	if src != nil && src.Body != nil {
		if _, ok := src.Body.(*responseBodyCapture); ok {
			return true
		}
	}
	return false
}

func isLogDetailCaptureActive(c *gin.Context) bool {
	if c == nil {
		return false
	}
	capture, ok := c.Get(logDetailContextKey)
	if !ok {
		return false
	}
	ldc, ok := capture.(*logDetailCapture)
	return ok && ldc != nil
}

func (r *responseBodyCapture) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	if n > 0 {
		r.buf.writeBytes(p[:n])
	}
	if err == io.EOF {
		r.flush()
	}
	return n, err
}

func (r *responseBodyCapture) Close() error {
	r.flush()
	return r.ReadCloser.Close()
}

func (r *responseBodyCapture) flush() {
	r.once.Do(func() {
		text := sanitizeCapturedText(r.buf.value(), r.contentType)
		updateLogDetailFields(r.requestId, map[string]interface{}{
			"raw_response_body": text.Text,
			"content_truncated": gorm.Expr("content_truncated OR ?", text.Truncated),
			"content_omitted":   gorm.Expr("content_omitted OR ?", text.Omitted),
			"omit_reason":       mergedOmitReasonExpr(text.Reason),
			"updated_at":        common.GetTimestamp(),
		})
	})
}

func (w *LogDetailResponseWriter) Write(data []byte) (int, error) {
	if w != nil && w.buf != nil {
		if w.mu != nil {
			w.mu.Lock()
			defer w.mu.Unlock()
		}
		w.buf.writeBytes(data)
	}
	return w.ResponseWriter.Write(data)
}

func (w *LogDetailResponseWriter) WriteString(data string) (int, error) {
	if w != nil && w.buf != nil {
		if w.mu != nil {
			w.mu.Lock()
			defer w.mu.Unlock()
		}
		w.buf.writeString(data)
	}
	return w.ResponseWriter.WriteString(data)
}

func (w *LogDetailResponseWriter) Status() int {
	return w.ResponseWriter.Status()
}

func (w *LogDetailResponseWriter) Size() int {
	return w.ResponseWriter.Size()
}

func (w *LogDetailResponseWriter) Written() bool {
	return w.ResponseWriter.Written()
}

func (w *LogDetailResponseWriter) WriteHeaderNow() {
	w.ResponseWriter.WriteHeaderNow()
}

func (w *LogDetailResponseWriter) Pusher() http.Pusher {
	return w.ResponseWriter.Pusher()
}

func (w *LogDetailResponseWriter) Flush() {
	w.ResponseWriter.Flush()
}

func (w *LogDetailResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return w.ResponseWriter.Hijack()
}

func (w *LogDetailResponseWriter) CloseNotify() <-chan bool {
	return w.ResponseWriter.CloseNotify()
}

func FlushCapturedLogDetailResponse(c *gin.Context, info *relaycommon.RelayInfo, statusCode int) {
	if c == nil {
		return
	}
	capture, ok := c.Get(logDetailContextKey)
	if !ok {
		return
	}
	ldc, ok := capture.(*logDetailCapture)
	if !ok || ldc == nil {
		return
	}
	ldc.mu.Lock()
	responseContentType := c.Writer.Header().Get("Content-Type")
	text := sanitizeCapturedText(ldc.responseBody.value(), responseContentType)
	ldc.mu.Unlock()
	if statusCode == 0 && c.Writer != nil {
		statusCode = c.Writer.Status()
	}
	meta := buildLogDetailMeta(c, info, statusCode)
	meta.ResponseContentType = responseContentType
	displayText := displayResponseDetailText(text, responseContentType, meta.IsStream)
	applyTextMeta(&meta, "response", displayText)
	metaJSON := marshalLogDetailMeta(meta)
	updateLogDetailFields(ldc.requestId, map[string]interface{}{
		"status_code":       statusCode,
		"is_stream":         meta.IsStream,
		"response_body":     displayText.Text,
		"request_params":    metaJSON,
		"content_truncated": gorm.Expr("content_truncated OR ?", text.Truncated || displayText.Truncated),
		"content_omitted":   gorm.Expr("content_omitted OR ?", text.Omitted || displayText.Omitted),
		"omit_reason":       mergedOmitReasonExpr(firstNonEmpty(text.Reason, displayText.Reason)),
		"updated_at":        common.GetTimestamp(),
	})
}

func CleanupLogDetailWithoutLog(c *gin.Context, info *relaycommon.RelayInfo) {
	if !isLogDetailCaptureActive(c) {
		return
	}
	requestId := requestIdFromContext(c, info)
	if requestId == "" || model.LOG_DB == nil {
		return
	}
	if err := model.DeleteLogDetailIfNoLog(requestId); err != nil {
		logger.LogError(c, "failed to cleanup orphan log detail: "+err.Error())
	}
}

func SetLogDetailError(c *gin.Context, info *relaycommon.RelayInfo, statusCode int, errText string) {
	if c == nil || strings.TrimSpace(errText) == "" {
		return
	}
	if !isLogDetailCaptureActive(c) {
		return
	}
	requestId := requestIdFromContext(c, info)
	if requestId == "" {
		return
	}
	text := sanitizeTextBody(errText, c.Writer.Header().Get("Content-Type"))
	meta := buildLogDetailMeta(c, info, statusCode)
	applyTextMeta(&meta, "error", text)
	metaJSON := marshalLogDetailMeta(meta)
	updateLogDetailFields(requestId, map[string]interface{}{
		"status_code":       statusCode,
		"error_body":        text.Text,
		"request_params":    metaJSON,
		"content_truncated": gorm.Expr("content_truncated OR ?", text.Truncated),
		"content_omitted":   gorm.Expr("content_omitted OR ?", text.Omitted),
		"omit_reason":       mergedOmitReasonExpr(text.Reason),
		"updated_at":        common.GetTimestamp(),
	})
}

func UpdateLogDetailMeta(c *gin.Context, info *relaycommon.RelayInfo, statusCode int, responseContentType string) {
	if c == nil {
		return
	}
	if !isLogDetailCaptureActive(c) {
		return
	}
	requestId := requestIdFromContext(c, info)
	if requestId == "" {
		return
	}
	meta := buildLogDetailMeta(c, info, statusCode)
	meta.ResponseContentType = responseContentType
	updateLogDetailFields(requestId, map[string]interface{}{
		"status_code":    statusCode,
		"is_stream":      meta.IsStream,
		"request_params": marshalLogDetailMeta(meta),
		"updated_at":     common.GetTimestamp(),
	})
}

func GetLogDetail(c *gin.Context, requestId string, isAdmin bool) (*model.LogDetail, error) {
	userId := 0
	if c != nil {
		userId = c.GetInt("id")
	}
	if err := model.CheckLogDetailAccess(requestId, userId, isAdmin); err != nil {
		return nil, err
	}
	detail, err := model.GetLogDetailByRequestId(requestId)
	if err != nil {
		return nil, err
	}
	if !isAdmin {
		sanitizeLogDetailForUser(detail)
	}
	return detail, nil
}

func sanitizeLogDetailForUser(detail *model.LogDetail) {
	if detail == nil || strings.TrimSpace(string(detail.RequestParams)) == "" {
		return
	}
	var params map[string]interface{}
	if err := common.UnmarshalJsonStr(string(detail.RequestParams), &params); err != nil {
		return
	}
	for _, key := range []string{
		"token_id",
		"channel_id",
		"channel_type",
		"channel_name",
		"request_conversion",
		"final_request_relay_format",
	} {
		delete(params, key)
	}
	data, err := common.Marshal(params)
	if err != nil {
		return
	}
	detail.RequestParams = model.LogDetailLargeText(data)
}

func extractRequestDetailText(c *gin.Context) logDetailText {
	if c == nil || c.Request == nil {
		return logDetailText{Omitted: true, Reason: "request context is empty"}
	}
	contentType := c.Request.Header.Get("Content-Type")
	storage, err := common.GetBodyStorage(c)
	if err != nil {
		if isMultipartContentType(contentType) && c.Request.MultipartForm != nil {
			return summarizeParsedMultipartForm(c.Request.MultipartForm, requestContentLength(c))
		}
		return logDetailText{Omitted: true, Reason: err.Error()}
	}
	if _, seekErr := storage.Seek(0, io.SeekStart); seekErr == nil {
		c.Request.Body = io.NopCloser(storage)
	}
	if isMultipartContentType(contentType) {
		text := summarizeMultipartText(storage, storage.Size(), contentType)
		if shouldUseParsedMultipartSummary(text, storage.Size(), c.Request.MultipartForm) {
			text = summarizeParsedMultipartForm(c.Request.MultipartForm, requestContentLength(c))
		}
		if _, seekErr := storage.Seek(0, io.SeekStart); seekErr == nil {
			c.Request.Body = io.NopCloser(storage)
		}
		return text
	}
	if !isTextContentType(contentType) && storage.Size() > 0 {
		return logDetailText{Original: int(storage.Size()), Omitted: true, Reason: "non-text request content-type " + contentType}
	}
	text, err := readLimitedStorageText(storage, contentType)
	if err != nil {
		return logDetailText{Omitted: true, Reason: err.Error()}
	}
	return text
}

func shouldUseParsedMultipartSummary(text logDetailText, storageSize int64, form *multipart.Form) bool {
	if !parsedMultipartFormHasContent(form) {
		return false
	}
	if text.Text == "" {
		return true
	}
	if storageSize == 0 {
		return true
	}
	return strings.Contains(text.Reason, "failed to parse multipart body")
}

func parsedMultipartFormHasContent(form *multipart.Form) bool {
	if form == nil {
		return false
	}
	for _, values := range form.Value {
		if len(values) > 0 {
			return true
		}
	}
	for _, files := range form.File {
		if len(files) > 0 {
			return true
		}
	}
	return false
}

func readLimitedStorageText(storage common.BodyStorage, contentType string) (logDetailText, error) {
	if _, err := storage.Seek(0, io.SeekStart); err != nil {
		return logDetailText{}, err
	}
	data, err := io.ReadAll(io.LimitReader(storage, int64(logDetailTextLimitBytes+1)))
	if err != nil {
		return logDetailText{}, err
	}
	text := sanitizeTextBody(string(data), contentType)
	text.Original = int(storage.Size())
	if int64(len(data)) < storage.Size() {
		text.Truncated = true
	}
	return text, nil
}

func sanitizeTextBody(text string, contentType string) logDetailText {
	result := newLimitedTextBuffer()
	if text == "" {
		return result.value()
	}
	sanitized := replaceMediaData(text)
	result.writeString(sanitized)
	return result.value()
}

func sanitizeCapturedText(text logDetailText, contentType string) logDetailText {
	if text.Omitted || text.Text == "" {
		return text
	}
	if !isTextContentType(contentType) {
		text.Text = ""
		text.Omitted = true
		text.Reason = "binary response content-type " + contentType
		return text
	}
	sanitized := sanitizeTextBody(text.Text, contentType)
	sanitized.Original = text.Original
	sanitized.Truncated = text.Truncated || sanitized.Truncated
	return sanitized
}

func displayResponseDetailText(text logDetailText, contentType string, isStream bool) logDetailText {
	if text.Omitted || text.Text == "" {
		return text
	}
	if !isStream && !isEventStreamContentType(contentType) {
		return text
	}
	display, ok := extractStreamOutputText(text.Text)
	if !ok || strings.TrimSpace(display) == "" {
		return text
	}
	result := sanitizeTextBody(display, "text/plain")
	result.Original = text.Original
	result.Truncated = text.Truncated || result.Truncated
	result.Omitted = text.Omitted
	result.Reason = text.Reason
	return result
}

func extractStreamOutputText(stream string) (string, bool) {
	var output strings.Builder
	eventData := make([]string, 0, 1)
	extracted := false

	flushEvent := func() {
		if len(eventData) == 0 {
			return
		}
		payload := strings.TrimSpace(strings.Join(eventData, "\n"))
		if appendStreamPayloadOutput(&output, payload) {
			extracted = true
		}
		eventData = eventData[:0]
	}

	handlePayload := func(payload string) {
		payload = strings.TrimSpace(payload)
		if payload == "" || strings.HasPrefix(payload, "[DONE]") {
			flushEvent()
			return
		}
		if len(eventData) > 0 {
			eventData = append(eventData, payload)
			combined := strings.TrimSpace(strings.Join(eventData, "\n"))
			if appendStreamPayloadOutput(&output, combined) {
				extracted = true
				eventData = eventData[:0]
			} else if isJSONStreamPayload(combined) {
				eventData = eventData[:0]
			}
			return
		}
		if appendStreamPayloadOutput(&output, payload) {
			extracted = true
			return
		}
		if isJSONStreamPayload(payload) {
			return
		}
		eventData = append(eventData, payload)
	}

	for _, line := range strings.Split(stream, "\n") {
		line = strings.TrimSuffix(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			flushEvent()
			continue
		}
		if strings.HasPrefix(trimmed, ":") {
			continue
		}
		if strings.HasPrefix(trimmed, "data:") {
			handlePayload(strings.TrimSpace(strings.TrimPrefix(trimmed, "data:")))
			continue
		}
	}
	flushEvent()

	return output.String(), extracted
}

func isJSONStreamPayload(payload string) bool {
	var data interface{}
	return common.UnmarshalJsonStr(payload, &data) == nil
}

func appendStreamPayloadOutput(output *strings.Builder, payload string) bool {
	if output == nil {
		return false
	}
	payload = strings.TrimSpace(payload)
	if payload == "" || strings.HasPrefix(payload, "[DONE]") {
		return false
	}

	before := output.Len()
	appendChatCompletionsStreamOutput(output, payload)
	appendCompletionsStreamOutput(output, payload)
	appendResponsesStreamOutput(output, payload)
	appendClaudeStreamOutput(output, payload)
	if output.Len() > before {
		return true
	}

	appendGenericStreamOutput(output, payload)
	return output.Len() > before
}

func appendChatCompletionsStreamOutput(output *strings.Builder, payload string) {
	var streamResponse dto.ChatCompletionsStreamResponse
	if err := common.UnmarshalJsonStr(payload, &streamResponse); err != nil {
		return
	}
	for _, choice := range streamResponse.Choices {
		appendOutputPart(output, choice.Delta.GetReasoningContent())
		appendOutputPart(output, choice.Delta.GetContentString())
	}
}

func appendCompletionsStreamOutput(output *strings.Builder, payload string) {
	var streamResponse dto.CompletionsStreamResponse
	if err := common.UnmarshalJsonStr(payload, &streamResponse); err != nil {
		return
	}
	for _, choice := range streamResponse.Choices {
		appendOutputPart(output, choice.Text)
	}
}

func appendResponsesStreamOutput(output *strings.Builder, payload string) {
	var streamResponse dto.ResponsesStreamResponse
	if err := common.UnmarshalJsonStr(payload, &streamResponse); err != nil {
		return
	}
	switch streamResponse.Type {
	case "response.output_text.delta":
		appendOutputPart(output, streamResponse.Delta)
	}
}

func appendClaudeStreamOutput(output *strings.Builder, payload string) {
	var streamResponse dto.ClaudeResponse
	if err := common.UnmarshalJsonStr(payload, &streamResponse); err != nil {
		return
	}
	if streamResponse.Type != "content_block_delta" || streamResponse.Delta == nil {
		return
	}
	appendOutputPart(output, streamResponse.Delta.GetText())
	if streamResponse.Delta.Thinking != nil {
		appendOutputPart(output, *streamResponse.Delta.Thinking)
	}
	appendOutputPart(output, streamResponse.Delta.Delta)
}

func appendGenericStreamOutput(output *strings.Builder, payload string) {
	var data map[string]interface{}
	if err := common.UnmarshalJsonStr(payload, &data); err != nil {
		return
	}
	if streamType, _ := data["type"].(string); streamType == "response.output_text.delta" {
		if delta, ok := data["delta"].(string); ok {
			appendOutputPart(output, delta)
			return
		}
	}
	if choices, ok := data["choices"].([]interface{}); ok {
		for _, item := range choices {
			choice, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if text, ok := choice["text"].(string); ok {
				appendOutputPart(output, text)
			}
			if delta, ok := choice["delta"].(map[string]interface{}); ok {
				appendOutputPartFromMap(output, delta, "reasoning_content")
				appendOutputPartFromMap(output, delta, "reasoning")
				appendOutputPartFromMap(output, delta, "content")
			}
		}
	}
	if delta, ok := data["delta"].(map[string]interface{}); ok {
		appendOutputPartFromMap(output, delta, "thinking")
		appendOutputPartFromMap(output, delta, "text")
		appendOutputPartFromMap(output, delta, "content")
	}
}

func appendOutputPartFromMap(output *strings.Builder, data map[string]interface{}, key string) {
	if value, ok := data[key].(string); ok {
		appendOutputPart(output, value)
	}
}

func appendOutputPart(output *strings.Builder, text string) {
	if output == nil || text == "" {
		return
	}
	output.WriteString(text)
}

func replaceMediaData(text string) string {
	return longBase64Pattern.ReplaceAllStringFunc(text, func(match string) string {
		if strings.HasPrefix(strings.ToLower(match), `"data:`) {
			return `"[omitted media data]"`
		}
		if len(match) < 4098 {
			return match
		}
		return `"[omitted large base64 text]"`
	})
}

func summarizeMultipartText(reader io.Reader, originalSize int64, contentType string) logDetailText {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || !strings.HasPrefix(mediaType, "multipart/") {
		return logDetailText{Original: int(originalSize), Omitted: true, Reason: "invalid multipart content-type"}
	}
	multipartBody := multipartReader(reader, params["boundary"])
	if multipartBody == nil {
		return logDetailText{Original: int(originalSize), Omitted: true, Reason: "multipart boundary missing"}
	}
	summary := make(map[string]interface{})
	fields := make(map[string][]string)
	omittedFiles := make([]string, 0)
	for {
		part, err := multipartBody.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return logDetailText{Original: int(originalSize), Omitted: true, Reason: "failed to parse multipart body"}
		}
		name := part.FormName()
		if name == "" {
			continue
		}
		if part.FileName() != "" {
			omittedFiles = append(omittedFiles, name)
			continue
		}
		value, _ := io.ReadAll(io.LimitReader(part, int64(logDetailTextLimitBytes+1)))
		fields[name] = append(fields[name], truncatePlainText(string(value), logDetailTextLimitBytes))
	}
	summary["text_fields"] = fields
	if len(omittedFiles) > 0 {
		summary["omitted_file_fields"] = omittedFiles
	}
	jsonData, err := common.Marshal(summary)
	if err != nil {
		return logDetailText{Original: int(originalSize), Omitted: true, Reason: "failed to marshal multipart summary"}
	}
	result := sanitizeTextBody(string(jsonData), "application/json")
	result.Original = int(originalSize)
	if len(omittedFiles) > 0 {
		result.Omitted = true
		result.Reason = "multipart form contains file content"
	}
	return result
}

func summarizeParsedMultipartForm(form *multipart.Form, originalSize int64) logDetailText {
	if form == nil {
		return logDetailText{Original: int(originalSize), Omitted: true, Reason: "multipart form is empty"}
	}
	summary := make(map[string]interface{})
	fields := make(map[string][]string)
	for name, values := range form.Value {
		for _, value := range values {
			fields[name] = append(fields[name], truncatePlainText(value, logDetailTextLimitBytes))
		}
	}
	omittedFiles := make([]string, 0)
	for name, files := range form.File {
		if len(files) > 0 {
			omittedFiles = append(omittedFiles, name)
		}
	}
	summary["text_fields"] = fields
	if len(omittedFiles) > 0 {
		summary["omitted_file_fields"] = omittedFiles
	}
	jsonData, err := common.Marshal(summary)
	if err != nil {
		return logDetailText{Original: int(originalSize), Omitted: true, Reason: "failed to marshal multipart summary"}
	}
	result := sanitizeTextBody(string(jsonData), "application/json")
	result.Original = int(originalSize)
	if len(omittedFiles) > 0 {
		result.Omitted = true
		result.Reason = "multipart form contains file content"
	}
	return result
}

func multipartReader(reader io.Reader, boundary string) *multipart.Reader {
	if strings.TrimSpace(boundary) == "" {
		return nil
	}
	return multipart.NewReader(reader, boundary)
}

func isMultipartContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return strings.Contains(strings.ToLower(contentType), "multipart/form-data")
	}
	return strings.HasPrefix(strings.ToLower(mediaType), "multipart/")
}

func isTextContentType(contentType string) bool {
	if contentType == "" {
		return true
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	mediaType = strings.ToLower(mediaType)
	if strings.HasPrefix(mediaType, "text/") {
		return true
	}
	switch mediaType {
	case "application/json", "application/x-ndjson", "application/xml", "application/x-www-form-urlencoded":
		return true
	}
	if strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml") {
		return true
	}
	return false
}

func isEventStreamContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	return strings.EqualFold(mediaType, "text/event-stream")
}

func buildLogDetailMeta(c *gin.Context, info *relaycommon.RelayInfo, statusCode int) logDetailMeta {
	meta := logDetailMeta{
		RequestID:         requestIdFromContext(c, info),
		UpstreamRequestID: "",
		Method:            requestMethod(c),
		Path:              requestPath(c),
		StatusCode:        statusCode,
		ContentLimitBytes: logDetailTextLimitBytes,
	}
	if c != nil {
		meta.UpstreamRequestID = c.GetString(common.UpstreamRequestIdKey)
		meta.UserID = c.GetInt("id")
		meta.TokenID = c.GetInt("token_id")
		meta.ChannelID = common.GetContextKeyInt(c, constant.ContextKeyChannelId)
		meta.ChannelType = common.GetContextKeyInt(c, constant.ContextKeyChannelType)
		meta.ChannelName = common.GetContextKeyString(c, constant.ContextKeyChannelName)
		if c.Request != nil {
			meta.RequestContentType = c.Request.Header.Get("Content-Type")
		}
	}
	if info != nil {
		meta.UserID = info.UserId
		meta.TokenID = info.TokenId
		if info.ChannelMeta != nil {
			meta.ChannelID = info.ChannelId
			meta.ChannelType = info.ChannelType
		}
		meta.Model = info.OriginModelName
		meta.RelayMode = info.RelayMode
		meta.RelayFormat = string(info.RelayFormat)
		meta.FinalRequestRelayFormat = string(info.GetFinalRequestRelayFormat())
		meta.IsStream = info.IsStream
		if len(info.RequestConversionChain) > 0 {
			meta.RequestConversion = make([]string, 0, len(info.RequestConversionChain))
			for _, format := range info.RequestConversionChain {
				meta.RequestConversion = append(meta.RequestConversion, string(format))
			}
		}
	}
	if c != nil && c.Writer != nil && statusCode == 0 {
		meta.StatusCode = c.Writer.Status()
	}
	return meta
}

func applyTextMeta(meta *logDetailMeta, field string, text logDetailText) {
	if meta == nil {
		return
	}
	switch field {
	case "request":
		meta.RequestBodyBytes = text.Original
		meta.RequestBodySavedBytes = len(text.Text)
		meta.RequestBodyTruncated = text.Truncated
		meta.RequestBodyOmitted = text.Omitted
		meta.RequestBodyOmitReason = text.Reason
	case "response":
		meta.ResponseBodyBytes = text.Original
		meta.ResponseBodySavedBytes = len(text.Text)
		meta.ResponseBodyTruncated = text.Truncated
		meta.ResponseBodyOmitted = text.Omitted
		meta.ResponseBodyOmitReason = text.Reason
	case "raw":
		meta.RawBodyBytes = text.Original
		meta.RawBodySavedBytes = len(text.Text)
		meta.RawBodyTruncated = text.Truncated
		meta.RawBodyOmitted = text.Omitted
		meta.RawBodyOmitReason = text.Reason
	case "error":
		meta.ErrorBodyBytes = text.Original
		meta.ErrorBodySavedBytes = len(text.Text)
		meta.ErrorBodyTruncated = text.Truncated
	}
}

func marshalLogDetailMeta(meta logDetailMeta) string {
	data, err := common.Marshal(meta)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func upsertLogDetail(c *gin.Context, detail model.LogDetail, columns []string) {
	if model.LOG_DB == nil {
		return
	}
	if err := model.LOG_DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "request_id"}},
		DoUpdates: clause.AssignmentColumns(columns),
	}).Create(&detail).Error; err != nil {
		logger.LogError(c, "failed to upsert log detail: "+err.Error())
	}
}

func updateLogDetailFields(requestId string, fields map[string]interface{}) {
	if model.LOG_DB == nil || requestId == "" || len(fields) == 0 {
		return
	}
	if err := model.LOG_DB.Model(&model.LogDetail{}).Where("request_id = ?", requestId).Updates(fields).Error; err != nil {
		common.SysError("failed to update log detail: " + err.Error())
	}
}

func mergedOmitReasonExpr(reason string) interface{} {
	reason = truncatePlainText(reason, 255)
	if reason == "" {
		return gorm.Expr("omit_reason")
	}
	return gorm.Expr("CASE WHEN omit_reason = '' THEN ? ELSE omit_reason END", reason)
}

func requestIdFromContext(c *gin.Context, info *relaycommon.RelayInfo) string {
	if c != nil {
		if requestId := c.GetString(common.RequestIdKey); requestId != "" {
			return requestId
		}
	}
	if info != nil {
		return info.RequestId
	}
	return ""
}

func requestPath(c *gin.Context) string {
	if c == nil || c.Request == nil || c.Request.URL == nil {
		return ""
	}
	return truncatePlainText(c.Request.URL.String(), 512)
}

func requestMethod(c *gin.Context) string {
	if c == nil || c.Request == nil {
		return ""
	}
	return truncatePlainText(c.Request.Method, 16)
}

func requestContentLength(c *gin.Context) int64 {
	if c == nil || c.Request == nil || c.Request.ContentLength < 0 {
		return 0
	}
	return c.Request.ContentLength
}

func truncatePlainText(text string, limit int) string {
	if limit <= 0 || len(text) <= limit {
		return text
	}
	return text[:limit]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
