// 本文件验证请求响应详情的采集、裁剪、流解析与持久化行为。
package service

import (
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/relaykit/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
	assert.Equal(t, defaultLogDetailTextLimitBytes, len(got.Text))
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

func TestStreamDisplayTextExtractsChatCompletionDeltas(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"id":"resp_1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant"},"finish_reason":null}]}`,
		``,
		`:`,
		`: PING`,
		`data: {"id":"resp_1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"下面"},"finish_reason":null}]}`,
		``,
		`data: {"id":"resp_1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"给"},"finish_reason":null}]}`,
		``,
		`data: {"id":"resp_1","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"出"},"finish_reason":null}]}`,
		`data: [DONE]`,
		``,
	}, "\n")
	text := logDetailText{
		Text:     raw,
		Original: len(raw),
	}

	got := displayResponseDetailText(text, "text/event-stream", true)

	assert.Equal(t, "下面给出", got.Text)
	assert.NotContains(t, got.Text, "data:")
	assert.NotContains(t, got.Text, "resp_1")
	assert.Equal(t, len(raw), got.Original)
}

func TestStreamDisplayTextExtractsReasoningAndResponsesDeltas(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"choices":[{"delta":{"reasoning_content":"思考","content":"回答"}}]}`,
		`data: {"type":"response.output_text.delta","delta":"内容"}`,
		`data: [DONE]`,
	}, "\n")
	text := logDetailText{
		Text:     raw,
		Original: len(raw),
	}

	got := displayResponseDetailText(text, "text/event-stream; charset=utf-8", true)

	assert.Equal(t, "思考回答内容", got.Text)
}

func TestStreamDisplayTextExtractsClaudeDeltaText(t *testing.T) {
	raw := strings.Join([]string{
		`event: content_block_delta`,
		`data: {"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}`,
		``,
	}, "\n")
	text := logDetailText{
		Text:     raw,
		Original: len(raw),
	}

	got := displayResponseDetailText(text, "text/event-stream", true)

	assert.Equal(t, "hello", got.Text)
}

func TestStreamDisplayTextFallsBackToRawWhenNoReadableText(t *testing.T) {
	raw := strings.Join([]string{
		`data: {"id":"resp_1","object":"chat.completion.chunk","choices":[{"delta":{"role":"assistant"}}]}`,
		`data: [DONE]`,
	}, "\n")
	text := logDetailText{
		Text:      raw,
		Original:  len(raw),
		Truncated: true,
	}

	got := displayResponseDetailText(text, "text/event-stream", true)

	assert.Equal(t, raw, got.Text)
	assert.True(t, got.Truncated)
}

func TestDisplayTextKeepsNonStreamJSON(t *testing.T) {
	raw := `{"choices":[{"message":{"content":"普通响应"}}]}`
	text := logDetailText{
		Text:     raw,
		Original: len(raw),
	}

	got := displayResponseDetailText(text, "application/json", false)

	assert.Equal(t, raw, got.Text)
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
