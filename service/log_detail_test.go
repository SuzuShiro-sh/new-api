// 本文件验证请求响应详情的采集、裁剪、流解析与持久化行为。
package service

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLogDetailArchiveCapturesTransportReplay(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		if calls.Add(1) == 2 {
			connection, _, err := w.(http.Hijacker).Hijack()
			require.NoError(t, err)
			require.NoError(t, connection.Close())
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	client := newRelayHTTPClient(server.Client().Transport)
	response, err := client.Get(server.URL)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("original"))
	archive := startLogDetailArchive(c)
	defer archive.cleanup()
	defer common.CleanupBodyStorage(c)
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, server.URL, strings.NewReader("replayed-body"))
	require.NoError(t, err)
	req.Header.Set("Idempotency-Key", "replay-test")
	response, err = client.Do(req)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, response.Body)
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	reader, complete, err := archive.finish(nil, 200)
	require.NoError(t, err)
	assert.True(t, complete)
	assert.Equal(t, int32(3), calls.Load())
	readerScan := bufio.NewScanner(reader)
	bodies := map[int64]string{}
	for readerScan.Scan() {
		var event logDetailArchiveEvent
		require.NoError(t, common.Unmarshal(readerScan.Bytes(), &event))
		if event.Stage == "upstream_request_body" {
			bodies[event.Replay] += string(event.Data)
		}
	}
	require.NoError(t, readerScan.Err())
	assert.Equal(t, map[int64]string{0: "replayed-body", 1: "replayed-body"}, bodies)
}

func TestLogDetailArchiveMarksUnfinishedBodyIncomplete(t *testing.T) {
	for _, closeBody := range []bool{false, true} {
		t.Run(fmt.Sprintf("closed=%t", closeBody), func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/", nil)
			archive := startLogDetailArchive(c)
			defer archive.cleanup()
			defer common.CleanupBodyStorage(c)
			body := archive.body(io.NopCloser(strings.NewReader("unfinished")), "upstream_response_body", 1, 0, -1)
			if closeBody {
				require.NoError(t, body.Close())
			}
			_, complete, err := archive.finish(nil, 200)
			require.NoError(t, err)
			assert.False(t, complete)
		})
	}
}

func BenchmarkLogDetailArchive(b *testing.B) {
	for _, size := range []int{1 << 20, 32 << 20} {
		b.Run(fmt.Sprintf("%dMiB", size>>20), func(b *testing.B) {
			chunk := bytes.Repeat([]byte{0, 255, 127, 3}, 8192)
			b.SetBytes(int64(size))
			b.ReportAllocs()
			for b.Loop() {
				_, file, err := common.CreateDiskCacheFile(common.DiskCacheTypeLogDetail)
				require.NoError(b, err)
				archive := &logDetailArchive{file: file, writer: bufio.NewWriterSize(file, 32*1024)}
				for range size / len(chunk) {
					archive.record(logDetailArchiveEvent{Stage: "upstream_response_body", Data: chunk})
				}
				_, complete, err := archive.finish(nil, 200)
				require.NoError(b, err)
				require.True(b, complete)
				archive.cleanup()
			}
		})
	}
}

func TestLogDetailWriterPreservesWriteDeadline(t *testing.T) {
	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		c.Writer = &LogDetailResponseWriter{ResponseWriter: c.Writer}
		err := http.NewResponseController(c.Writer).SetWriteDeadline(time.Now().Add(time.Minute))
		if err != nil {
			c.String(500, "%v", err)
			return
		}
		c.Status(http.StatusOK)
	})
	server := httptest.NewServer(router)
	defer server.Close()
	response, err := server.Client().Get(server.URL)
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusOK, response.StatusCode)
}

func TestLogDetailClientJSONErrorUsesClientContentType(t *testing.T) {
	truncate(t)
	require.NoError(t, model.LOG_DB.Create(&model.Log{RequestId: "client-json-error", UserId: 1}).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`))
	c.Set(common.RequestIdKey, "client-json-error")
	common.SetContextKey(c, constant.ContextKeyTokenLogDetailEnabled, true)
	info := &relaycommon.RelayInfo{RequestId: "client-json-error", UserId: 1}
	CaptureRelayRequestDetail(c, info)
	defer common.CleanupBodyStorage(c)
	resp := WrapLogDetailResponse(c, &http.Response{StatusCode: 502, Header: http.Header{"Content-Type": {"application/octet-stream"}}, Body: io.NopCloser(strings.NewReader("upstream error"))})
	_, err := io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	c.JSON(502, gin.H{"error": "failed"})
	FlushCapturedLogDetailResponse(c, info, 502)
	detail, err := model.GetLogDetailByRequestId(info.RequestId)
	require.NoError(t, err)
	assert.JSONEq(t, `{"error":"failed"}`, string(detail.ResponseBody))
}

func TestLogDetailArchiveRetainsBinaryMultipartRetriesAndClientStream(t *testing.T) {
	truncate(t)
	require.NoError(t, model.LOG_DB.AutoMigrate(&model.LogDetailArchiveChunk{}))
	previous := common.LogDetailFullCaptureEnabled
	common.LogDetailFullCaptureEnabled = true
	t.Cleanup(func() { common.LogDetailFullCaptureEnabled = previous })
	var request bytes.Buffer
	multipartWriter := multipart.NewWriter(&request)
	part, err := multipartWriter.CreateFormFile("image", "pixel.bin")
	require.NoError(t, err)
	image := bytes.Repeat([]byte{0, 255, 1, 128}, 100_000)
	_, err = part.Write(image)
	require.NoError(t, err)
	require.NoError(t, multipartWriter.Close())
	requestBytes := bytes.Clone(request.Bytes())
	require.NoError(t, model.LOG_DB.Create(&model.Log{RequestId: "full-archive", UserId: 1}).Error)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits?api_key=synthetic-secret", bytes.NewReader(requestBytes))
	c.Request.Header.Set("Content-Type", multipartWriter.FormDataContentType())
	c.Request.Header.Set("Authorization", "Bearer synthetic-secret")
	c.Set(common.RequestIdKey, "full-archive")
	common.SetContextKey(c, constant.ContextKeyTokenLogDetailEnabled, true)
	info := &relaycommon.RelayInfo{RequestId: "full-archive", UserId: 1, IsStream: true}
	CaptureRelayRequestDetail(c, info)
	defer common.CleanupBodyStorage(c)
	archive := getLogDetailCapture(c).archive
	require.NotNil(t, archive)
	spoolPath := archive.file.Name()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		assert.Equal(t, image, body)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(502)
		_, _ = w.Write(image)
	}))
	defer server.Close()
	configureSSRFTestFetchSetting(t)
	serverURL, err := url.Parse(server.URL)
	require.NoError(t, err)
	fetchSetting := system_setting.GetFetchSetting()
	fetchSetting.AllowPrivateIp = true
	fetchSetting.AllowedPorts = []string{serverURL.Port()}
	protected := newProtectedFetchHTTPClientWithProxy(nil, nil, nil, func(*http.Request) (*url.URL, error) { return nil, nil })
	defer protected.CloseIdleConnections()
	for _, client := range []*http.Client{newRelayHTTPClient(server.Client().Transport), protected} {
		req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, server.URL, bytes.NewReader(image))
		require.NoError(t, err)
		resp, err := client.Do(req)
		require.NoError(t, err)
		_, err = io.Copy(io.Discard, resp.Body)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
	}
	stream := "event: response.output_item.added\ndata: {\"seed\":9007199254740993,\"type\":\"function_call\"}\n\ndata: [DONE]\n\n"
	c.Header("Content-Type", "text/event-stream")
	_, err = c.Writer.WriteString(stream)
	require.NoError(t, err)
	FlushCapturedLogDetailResponse(c, info, 200)
	assert.NoFileExists(t, spoolPath)
	detail, err := model.GetLogDetailByRequestId(info.RequestId)
	require.NoError(t, err)
	require.True(t, detail.ArchiveComplete)
	assert.Equal(t, stream, string(detail.ResponseBody))
	var compressed bytes.Buffer
	require.NoError(t, model.WriteLogDetailArchive(context.Background(), info.RequestId, detail.ArchiveBytes, &compressed))
	reader, err := gzip.NewReader(&compressed)
	require.NoError(t, err)
	defer reader.Close()
	scanner := bufio.NewScanner(reader)
	byStream := map[string][]byte{}
	for scanner.Scan() {
		assert.NotContains(t, scanner.Text(), "synthetic-secret")
		var event logDetailArchiveEvent
		require.NoError(t, common.Unmarshal(scanner.Bytes(), &event))
		assert.LessOrEqual(t, len(event.Data), 32*1024)
		key := fmt.Sprintf("%s:%d", event.Stage, event.Exchange)
		byStream[key] = append(byStream[key], event.Data...)
	}
	require.NoError(t, scanner.Err())
	assert.Equal(t, requestBytes, byStream["client_request_body:0"])
	assert.Equal(t, []byte(stream), byStream["client_response_body:0"])
	for exchange := 1; exchange <= 2; exchange++ {
		assert.Equal(t, image, byStream[fmt.Sprintf("upstream_request_body:%d", exchange)])
		assert.Equal(t, image, byStream[fmt.Sprintf("upstream_response_body:%d", exchange)])
	}
}

func TestDeduplicateRawResponseRemovesOnlyIdenticalBody(t *testing.T) {
	raw := logDetailText{Text: `{"id":"same"}`, Original: 128, Truncated: true}
	client := logDetailText{Text: `{"id":"same"}`}

	deduplicated, matched := deduplicateRawResponse(raw, client)
	assert.True(t, matched)
	assert.Empty(t, deduplicated.Text)
	assert.Equal(t, 128, deduplicated.Original)
	assert.True(t, deduplicated.Truncated)

	different, matched := deduplicateRawResponse(raw, logDetailText{Text: `{"id":"different"}`})
	assert.False(t, matched)
	assert.Equal(t, raw, different)
}

func TestReplaceMediaDataOmitsLongMediaAndBase64(t *testing.T) {
	media := `"data:image/png;base64,` + strings.Repeat("a", 256) + `"`
	base64 := `"` + strings.Repeat("A", 4098) + `"`
	got := replaceMediaData(`{"image":` + media + `,"blob":` + base64 + `}`)

	assert.NotContains(t, got, "data:image/png;base64")
	assert.NotContains(t, got, strings.Repeat("A", 128))
}

func TestSanitizeTextBodyOmitsTruncatedMediaDataURL(t *testing.T) {
	mediaURL := `"data:image/jpeg;base64,/9j/` + strings.Repeat("A", defaultLogDetailTextLimitBytes)
	body := `{"messages":[{"content":[{"type":"image_url","image_url":{"url":` + mediaURL

	got := sanitizeTextBody(body, "application/json")

	assert.NotContains(t, got.Text, "data:image/jpeg;base64")
	assert.NotContains(t, got.Text, strings.Repeat("A", 128))
	assert.Contains(t, got.Text, "[omitted media data]")
}

func TestReplaceMediaDataOmitsEscapedAndUppercaseMediaDataURL(t *testing.T) {
	body := `{"url":"DATA:image\/jpeg;base64,` + strings.Repeat("A", 512) + `"}`

	got := replaceMediaData(body)

	assert.NotContains(t, got, "DATA:image")
	assert.NotContains(t, got, `image\/jpeg`)
	assert.Contains(t, got, "[omitted media data]")
}

func TestReplaceMediaDataOmitsLongBase64URLText(t *testing.T) {
	body := `{"b64_json":"` + strings.Repeat("A-_", 1400) + `"}`

	got := replaceMediaData(body)

	assert.NotContains(t, got, strings.Repeat("A-_", 128))
	assert.Contains(t, got, "[omitted large base64 text]")
}

func TestSanitizeTextBodyTruncatesAtLimit(t *testing.T) {
	got := sanitizeTextBody(strings.Repeat("a", defaultLogDetailTextLimitBytes+1), "application/json")

	assert.True(t, got.Truncated)
	assert.Equal(t, currentLogDetailTextLimitBytes(), len(got.Text))
}

func TestSanitizeLogDetailForUserRemovesInternalParams(t *testing.T) {
	detail := &model.LogDetail{
		RequestParams: model.LogDetailLargeText(`{"request_id":"req_1","token_id":1,"channel_id":2,"channel_type":3,"channel_name":"private","request_conversion":["openai"],"final_request_relay_format":"openai","model":"gpt"}`),
	}

	sanitizeLogDetailForUser(detail)

	for _, forbidden := range []string{
		"token_id",
		"channel_id",
		"channel_type",
		"channel_name",
		"request_conversion",
		"final_request_relay_format",
	} {
		assert.NotContains(t, string(detail.RequestParams), forbidden)
	}
	assert.Contains(t, string(detail.RequestParams), `"model":"gpt"`)
}

func TestGetLogDetailKeepsUpstreamDiagnosticsAdminOnly(t *testing.T) {
	truncate(t)
	const clientResponse = `{"error":{"message":"upstream request failed"}}`
	const rawResponse = `{"error":"connection to https://private-upstream.invalid failed"}`
	for _, tc := range []struct {
		name   string
		legacy bool
		params string
	}{
		{name: "compressed", params: `{"model":"test","channel_id":2}`},
		{name: "legacy", legacy: true, params: `{"model":"test","channel_id":2}`},
		{name: "legacy without metadata", legacy: true},
		{name: "legacy malformed metadata", legacy: true, params: "invalid metadata"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestID := "private_detail_" + tc.name
			require.NoError(t, model.LOG_DB.Create(&model.Log{RequestId: requestID, UserId: 1}).Error)
			detail := &model.LogDetail{
				RequestId: requestID, UserId: 1, StatusCode: http.StatusBadGateway,
				RequestBody: "user prompt", RequestParams: model.LogDetailLargeText(tc.params),
				ResponseBody: clientResponse, RawResponseBody: rawResponse, ErrorBody: rawResponse,
			}
			if tc.legacy {
				require.NoError(t, model.LOG_DB.Table("log_details").Create(map[string]any{
					"request_id": requestID, "user_id": 1, "status_code": http.StatusBadGateway,
					"request_body": "user prompt", "request_params": tc.params,
					"response_body": clientResponse, "raw_response_body": rawResponse, "error_body": rawResponse,
				}).Error)
			} else {
				require.NoError(t, model.LOG_DB.Create(detail).Error)
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Set("id", 1)
			userDetail, err := GetLogDetail(ctx, requestID, false)
			require.NoError(t, err)
			assert.Empty(t, userDetail.RawResponseBody)
			assert.Empty(t, userDetail.ErrorBody)
			assert.Equal(t, clientResponse, string(userDetail.ResponseBody))
			assert.Equal(t, "user prompt", string(userDetail.RequestBody))
			assert.NotContains(t, string(userDetail.RequestParams), "channel_id")

			adminDetail, err := GetLogDetail(ctx, requestID, true)
			require.NoError(t, err)
			assert.Equal(t, rawResponse, string(adminDetail.RawResponseBody))
			assert.Equal(t, rawResponse, string(adminDetail.ErrorBody))
			ctx.Set("id", 2)
			_, err = GetLogDetail(ctx, requestID, false)
			require.Error(t, err)
		})
	}
}

func TestHasUpstreamRawCaptureDetectsWrappedResponse(t *testing.T) {
	resp := &http.Response{
		Body: &responseBodyCapture{
			ReadCloser: io.NopCloser(strings.NewReader("raw")),
		},
	}

	assert.True(t, hasUpstreamRawCapture(nil, resp))
}

func TestHasUpstreamRawCaptureDetectsContextMarker(t *testing.T) {
	c := &gin.Context{}
	c.Set(logDetailRawCaptureMarkerKey, true)

	assert.True(t, hasUpstreamRawCapture(c, nil))
}

func TestBuildLogDetailMetaHandlesMissingChannelMeta(t *testing.T) {
	c := &gin.Context{}
	c.Set(string(constant.ContextKeyChannelId), 11)
	c.Set(string(constant.ContextKeyChannelType), 22)
	c.Set(string(constant.ContextKeyChannelName), "context-channel")
	info := &relaycommon.RelayInfo{
		UserId:  33,
		TokenId: 44,
	}

	meta := buildLogDetailMeta(c, info, 200)

	assert.Equal(t, 33, meta.UserID)
	assert.Equal(t, 44, meta.TokenID)
	assert.Equal(t, 11, meta.ChannelID)
	assert.Equal(t, 22, meta.ChannelType)
	assert.Equal(t, "context-channel", meta.ChannelName)
}

func TestBuildLogDetailMetaUsesRelayChannelMetaWhenPresent(t *testing.T) {
	c := &gin.Context{}
	c.Set(string(constant.ContextKeyChannelId), 11)
	c.Set(string(constant.ContextKeyChannelType), 22)
	info := &relaycommon.RelayInfo{
		ChannelMeta: &relaycommon.ChannelMeta{
			ChannelId:   55,
			ChannelType: 66,
		},
	}

	meta := buildLogDetailMeta(c, info, 200)

	assert.Equal(t, 55, meta.ChannelID)
	assert.Equal(t, 66, meta.ChannelType)
}

func TestRequestPathOmitsQueryParameters(t *testing.T) {
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions?api_key=secret&trace=1", http.NoBody)

	assert.Equal(t, "/v1/chat/completions", requestPath(ctx))
	meta := buildLogDetailMeta(ctx, &relaycommon.RelayInfo{}, http.StatusOK)
	assert.Equal(t, "/v1/chat/completions", meta.Path)
	assert.NotContains(t, marshalLogDetailMeta(meta), "secret")
}

func TestFlushCapturedLogDetailPersistsOnlyPath(t *testing.T) {
	truncate(t)
	require.NoError(t, model.LOG_DB.Create(&model.Log{
		UserId:    1,
		CreatedAt: 10,
		Type:      model.LogTypeConsume,
		RequestId: "req_path_without_query",
	}).Error)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(
		http.MethodPost,
		"/v1/chat/completions?api_key=secret",
		strings.NewReader(`{"model":"gpt-test","messages":[]}`),
	)
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set(common.RequestIdKey, "req_path_without_query")
	common.SetContextKey(ctx, constant.ContextKeyTokenLogDetailEnabled, true)
	info := &relaycommon.RelayInfo{
		RequestId:       "req_path_without_query",
		UserId:          1,
		OriginModelName: "gpt-test",
		RelayFormat:     types.RelayFormatOpenAI,
	}

	CaptureRelayRequestDetail(ctx, info)
	_, err := ctx.Writer.Write([]byte(`{"id":"response"}`))
	require.NoError(t, err)
	FlushCapturedLogDetailResponse(ctx, info, http.StatusOK)

	detail, err := model.GetLogDetailByRequestId("req_path_without_query")
	require.NoError(t, err)
	assert.Equal(t, "/v1/chat/completions", detail.RequestPath)
	assert.NotContains(t, string(detail.RequestParams), "secret")
}

func TestCaptureRelayRequestDetailSkipsWhenConsumeLogDisabled(t *testing.T) {
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldLogDetailEnabled := common.LogDetailEnabled
	common.LogConsumeEnabled = false
	common.LogDetailEnabled = true
	defer func() {
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.LogDetailEnabled = oldLogDetailEnabled
	}()

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt","messages":[]}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set(common.RequestIdKey, "req_skip_detail")
	common.SetContextKey(c, constant.ContextKeyTokenLogDetailEnabled, true)
	originalBody := io.NopCloser(strings.NewReader(`{"id":"resp"}`))
	resp := &http.Response{
		Header: make(http.Header),
		Body:   originalBody,
	}
	info := &relaycommon.RelayInfo{
		RequestId:   "req_skip_detail",
		UserId:      1,
		RelayFormat: types.RelayFormatOpenAI,
	}

	CaptureRelayRequestDetail(c, info)
	_, captured := c.Get(logDetailContextKey)
	assert.False(t, captured)
	_, wrappedWriter := c.Writer.(*LogDetailResponseWriter)
	assert.False(t, wrappedWriter)

	wrapped := WrapLogDetailResponse(c, resp)
	_, capturedBody := wrapped.Body.(*responseBodyCapture)
	assert.False(t, capturedBody)
	assert.Equal(t, originalBody, wrapped.Body)
}

func TestCaptureRelayRequestDetailSkipsWhenDetailCaptureDisabled(t *testing.T) {
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldLogDetailEnabled := common.LogDetailEnabled
	common.LogConsumeEnabled = true
	common.LogDetailEnabled = false
	t.Cleanup(func() {
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.LogDetailEnabled = oldLogDetailEnabled
	})

	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt"}`))
	c.Set(common.RequestIdKey, "req_detail_disabled")
	common.SetContextKey(c, constant.ContextKeyTokenLogDetailEnabled, true)

	CaptureRelayRequestDetail(c, &relaycommon.RelayInfo{
		RequestId:   "req_detail_disabled",
		UserId:      1,
		RelayFormat: types.RelayFormatOpenAI,
	})

	_, captured := c.Get(logDetailContextKey)
	assert.False(t, captured)
	_, wrapped := c.Writer.(*LogDetailResponseWriter)
	assert.False(t, wrapped)
}

// TestCaptureRelayRequestDetailRequiresTokenOptIn 验证详情采集默认关闭且只能由令牌显式开启.
func TestCaptureRelayRequestDetailRequiresTokenOptIn(t *testing.T) {
	oldLogConsumeEnabled := common.LogConsumeEnabled
	oldLogDetailEnabled := common.LogDetailEnabled
	common.LogConsumeEnabled = true
	common.LogDetailEnabled = true
	t.Cleanup(func() {
		common.LogConsumeEnabled = oldLogConsumeEnabled
		common.LogDetailEnabled = oldLogDetailEnabled
	})

	for _, test := range []struct {
		name        string
		setContext  bool
		tokenOptIn  bool
		wantCapture bool
	}{
		{name: "missing token setting defaults to disabled"},
		{name: "explicitly disabled", setContext: true},
		{name: "explicitly enabled", setContext: true, tokenOptIn: true, wantCapture: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(recorder)
			ctx.Request = httptest.NewRequest(
				http.MethodPost,
				"/v1/chat/completions",
				strings.NewReader(`{"model":"gpt"}`),
			)
			ctx.Set(common.RequestIdKey, "req_token_detail_setting")
			if test.setContext {
				common.SetContextKey(ctx, constant.ContextKeyTokenLogDetailEnabled, test.tokenOptIn)
			}

			CaptureRelayRequestDetail(ctx, &relaycommon.RelayInfo{
				RequestId:   "req_token_detail_setting",
				UserId:      1,
				RelayFormat: types.RelayFormatOpenAI,
			})

			_, captured := ctx.Get(logDetailContextKey)
			assert.Equal(t, test.wantCapture, captured)
			_, wrapped := ctx.Writer.(*LogDetailResponseWriter)
			assert.Equal(t, test.wantCapture, wrapped)
			if test.wantCapture {
				require.NotNil(t, getLogDetailCapture(ctx))
			}
		})
	}
}

func TestExtractRequestDetailTextUsesParsedMultipartForm(t *testing.T) {
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/edits", http.NoBody)
	c.Request.Header.Set("Content-Type", "multipart/form-data; boundary=already-consumed")
	c.Request.ContentLength = 123
	c.Request.MultipartForm = &multipart.Form{
		Value: map[string][]string{
			"model":  {"gpt-image-1"},
			"prompt": {"edit this image"},
		},
		File: map[string][]*multipart.FileHeader{
			"image": {
				{Filename: "input.png"},
			},
		},
	}

	got := extractRequestDetailText(c)

	assert.Contains(t, got.Text, `"prompt":["edit this image"]`)
	assert.Contains(t, got.Text, `"omitted_file_fields":["image"]`)
	assert.True(t, got.Omitted)
	assert.Equal(t, "multipart form contains file content", got.Reason)
	assert.Equal(t, 123, got.Original)
}

func TestExtractRequestDetailTextPreservesRelayBody(t *testing.T) {
	for _, contentType := range []string{"application/json", "multipart/form-data; boundary=detail"} {
		t.Run(contentType, func(t *testing.T) {
			body := `{"model":"test","prompt":"keep the relay body"}`
			if strings.HasPrefix(contentType, "multipart/") {
				body = "--detail\r\nContent-Disposition: form-data; name=\"prompt\"\r\n\r\nkeep the relay body\r\n--detail--\r\n"
			}
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ctx.Request = httptest.NewRequest(http.MethodPost, "/v1/test", strings.NewReader(body))
			ctx.Request.Header.Set("Content-Type", contentType)
			storage, err := common.GetBodyStorage(ctx)
			require.NoError(t, err)
			t.Cleanup(func() { common.CleanupBodyStorage(ctx) })
			ctx.Request.Body = io.NopCloser(storage)
			// 模拟已有消费者读过前缀；日志采集不得移动或替换转发方的读取器。
			prefix := make([]byte, 3)
			_, err = io.ReadFull(ctx.Request.Body, prefix)
			require.NoError(t, err)

			detail := extractRequestDetailText(ctx)
			assert.Contains(t, detail.Text, "keep the relay body")
			remainder, err := io.ReadAll(ctx.Request.Body)
			require.NoError(t, err)
			assert.Equal(t, body[3:], string(remainder))
		})
	}
}

func TestCapturedLogDetailTruncationPreservesUTF8(t *testing.T) {
	for _, limit := range []int{1, 2, 3, 4, 5} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			buf := &limitedTextBuffer{limit: limit}
			data := []byte("中文")
			buf.writeBytes(data[:1])
			buf.writeBytes(data[1:])
			got := sanitizeCapturedText(buf.value(), "text/plain")
			assert.True(t, utf8.ValidString(got.Text))
			assert.LessOrEqual(t, len(got.Text), limit)
			assert.True(t, got.Truncated)
			assert.True(t, utf8.ValidString(truncatePlainText("中文", limit)))
		})
	}
}
