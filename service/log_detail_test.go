package service

import (
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
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

	if strings.Contains(got, "data:image/png;base64") {
		t.Fatalf("expected media data URI to be omitted, got %q", got)
	}
	if strings.Contains(got, strings.Repeat("A", 128)) {
		t.Fatalf("expected large base64 text to be omitted, got %q", got)
	}
}

func TestSanitizeTextBodyOmitsTruncatedMediaDataURL(t *testing.T) {
	mediaURL := `"data:image/jpeg;base64,/9j/` + strings.Repeat("A", defaultLogDetailTextLimitBytes)
	body := `{"messages":[{"content":[{"type":"image_url","image_url":{"url":` + mediaURL

	got := sanitizeTextBody(body, "application/json")

	if strings.Contains(got.Text, "data:image/jpeg;base64") {
		t.Fatalf("expected truncated media data URL to be omitted, got prefix %q", truncatePlainText(got.Text, 256))
	}
	if strings.Contains(got.Text, strings.Repeat("A", 128)) {
		t.Fatalf("expected truncated media base64 payload to be omitted, got prefix %q", truncatePlainText(got.Text, 256))
	}
	if !strings.Contains(got.Text, "[omitted media data]") {
		t.Fatalf("expected omitted media marker, got prefix %q", truncatePlainText(got.Text, 256))
	}
}

func TestReplaceMediaDataOmitsEscapedAndUppercaseMediaDataURL(t *testing.T) {
	body := `{"url":"DATA:image\/jpeg;base64,` + strings.Repeat("A", 512) + `"}`

	got := replaceMediaData(body)

	if strings.Contains(got, "DATA:image") || strings.Contains(got, `image\/jpeg`) {
		t.Fatalf("expected escaped uppercase media data URL to be omitted, got %q", got)
	}
	if !strings.Contains(got, "[omitted media data]") {
		t.Fatalf("expected omitted media marker, got %q", got)
	}
}

func TestReplaceMediaDataOmitsLongBase64URLText(t *testing.T) {
	body := `{"b64_json":"` + strings.Repeat("A-_", 1400) + `"}`

	got := replaceMediaData(body)

	if strings.Contains(got, strings.Repeat("A-_", 128)) {
		t.Fatalf("expected long base64url text to be omitted, got prefix %q", truncatePlainText(got, 256))
	}
	if !strings.Contains(got, "[omitted large base64 text]") {
		t.Fatalf("expected omitted large base64 marker, got %q", got)
	}
}

func TestSanitizeTextBodyTruncatesAtLimit(t *testing.T) {
	got := sanitizeTextBody(strings.Repeat("a", defaultLogDetailTextLimitBytes+1), "application/json")

	if !got.Truncated {
		t.Fatal("expected long text body to be marked truncated")
	}
	if len(got.Text) != defaultLogDetailTextLimitBytes {
		t.Fatalf("expected saved text length %d, got %d", defaultLogDetailTextLimitBytes, len(got.Text))
	}
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
		if strings.Contains(string(detail.RequestParams), forbidden) {
			t.Fatalf("expected %s to be removed from user params: %s", forbidden, detail.RequestParams)
		}
	}
	if !strings.Contains(string(detail.RequestParams), `"model":"gpt"`) {
		t.Fatalf("expected non-sensitive params to remain: %s", detail.RequestParams)
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

	if got.Text != "下面给出" {
		t.Fatalf("expected readable stream output, got %q", got.Text)
	}
	if strings.Contains(got.Text, "data:") || strings.Contains(got.Text, "resp_1") {
		t.Fatalf("expected SSE framing and metadata to be removed, got %q", got.Text)
	}
	if got.Original != len(raw) {
		t.Fatalf("expected original byte count to be preserved, got %d", got.Original)
	}
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

	if got.Text != "思考回答内容" {
		t.Fatalf("expected reasoning and response deltas to be extracted, got %q", got.Text)
	}
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

	if got.Text != "hello" {
		t.Fatalf("expected Claude delta text to be extracted, got %q", got.Text)
	}
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

	if got.Text != raw {
		t.Fatalf("expected raw stream fallback when no text delta exists, got %q", got.Text)
	}
	if !got.Truncated {
		t.Fatal("expected fallback metadata to preserve truncation")
	}
}

func TestDisplayTextKeepsNonStreamJSON(t *testing.T) {
	raw := `{"choices":[{"message":{"content":"普通响应"}}]}`
	text := logDetailText{
		Text:     raw,
		Original: len(raw),
	}

	got := displayResponseDetailText(text, "application/json", false)

	if got.Text != raw {
		t.Fatalf("expected non-stream JSON to remain unchanged, got %q", got.Text)
	}
}

func TestHasUpstreamRawCaptureDetectsWrappedResponse(t *testing.T) {
	resp := &http.Response{
		Body: &responseBodyCapture{
			ReadCloser: io.NopCloser(strings.NewReader("raw")),
		},
	}

	if !hasUpstreamRawCapture(nil, resp) {
		t.Fatal("expected wrapped upstream response body to be treated as raw capture")
	}
}

func TestHasUpstreamRawCaptureDetectsContextMarker(t *testing.T) {
	c := &gin.Context{}
	c.Set(logDetailRawCaptureMarkerKey, true)

	if !hasUpstreamRawCapture(c, nil) {
		t.Fatal("expected context raw capture marker to be detected")
	}
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

	if meta.UserID != 33 {
		t.Fatalf("expected user id from relay info, got %d", meta.UserID)
	}
	if meta.TokenID != 44 {
		t.Fatalf("expected token id from relay info, got %d", meta.TokenID)
	}
	if meta.ChannelID != 11 {
		t.Fatalf("expected channel id from context when ChannelMeta is nil, got %d", meta.ChannelID)
	}
	if meta.ChannelType != 22 {
		t.Fatalf("expected channel type from context when ChannelMeta is nil, got %d", meta.ChannelType)
	}
	if meta.ChannelName != "context-channel" {
		t.Fatalf("expected channel name from context, got %q", meta.ChannelName)
	}
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

	if meta.ChannelID != 55 {
		t.Fatalf("expected channel id from relay info ChannelMeta, got %d", meta.ChannelID)
	}
	if meta.ChannelType != 66 {
		t.Fatalf("expected channel type from relay info ChannelMeta, got %d", meta.ChannelType)
	}
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
	if _, ok := c.Get(logDetailContextKey); ok {
		t.Fatal("expected log detail capture context to be absent when consume logs are disabled")
	}
	if _, ok := c.Writer.(*LogDetailResponseWriter); ok {
		t.Fatal("expected response writer not to be wrapped when consume logs are disabled")
	}

	wrapped := WrapLogDetailResponse(c, resp)
	if _, ok := wrapped.Body.(*responseBodyCapture); ok {
		t.Fatal("expected upstream response body not to be wrapped without active log detail capture")
	}
	if wrapped.Body != originalBody {
		t.Fatal("expected upstream response body to remain unchanged without active log detail capture")
	}
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

	if !strings.Contains(got.Text, `"prompt":["edit this image"]`) {
		t.Fatalf("expected parsed multipart text fields to be captured, got %s", got.Text)
	}
	if !strings.Contains(got.Text, `"omitted_file_fields":["image"]`) {
		t.Fatalf("expected parsed multipart file fields to be summarized, got %s", got.Text)
	}
	if !got.Omitted {
		t.Fatal("expected multipart file content to be marked omitted")
	}
	if got.Reason != "multipart form contains file content" {
		t.Fatalf("expected multipart omit reason, got %q", got.Reason)
	}
	if got.Original != 123 {
		t.Fatalf("expected original request content length to be preserved, got %d", got.Original)
	}
}
