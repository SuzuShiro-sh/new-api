package service

import (
	"bufio"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

type logDetailArchiveContextKey struct{}

// 每行最多包含 32 KiB 原始正文。Data 的 JSON 编码为 Base64，保留二进制与无效 UTF-8。
type logDetailArchiveEvent struct {
	Version     int               `json:"version"`
	Sequence    int64             `json:"sequence"`
	Time        string            `json:"time"`
	Stage       string            `json:"stage"`
	Exchange    int64             `json:"exchange,omitempty"`
	Method      string            `json:"method,omitempty"`
	URL         string            `json:"url,omitempty"`
	Headers     http.Header       `json:"headers,omitempty"`
	Status      int               `json:"status,omitempty"`
	Data        []byte            `json:"data_base64,omitempty"`
	End         bool              `json:"end,omitempty"`
	Error       string            `json:"error,omitempty"`
	MessageType int               `json:"message_type,omitempty"`
	MessageID   int64             `json:"message_id,omitempty"`
	Replay      int64             `json:"replay,omitempty"`
	Metadata    common.RawMessage `json:"metadata,omitempty"`
	Host        string            `json:"host,omitempty"`
	Protocol    string            `json:"protocol,omitempty"`
}

type logDetailArchive struct {
	mu               sync.Mutex
	file             *os.File
	writer           *bufio.Writer
	sequence         int64
	exchange         int64
	err              error
	closed           bool
	incomplete       bool
	pendingBodies    int
	upstreamObserved bool
}

func (a *logDetailArchive) record(event logDetailArchiveEvent) {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed || a.err != nil {
		return
	}
	event.Version = 1
	event.Time = time.Now().UTC().Format(time.RFC3339Nano)
	if event.MessageType != 0 {
		event.MessageID = a.sequence + 1
	}
	for {
		data := event.Data
		chunk := data[:min(len(data), 32*1024)]
		event.Data = nil
		if event.MessageType != 0 {
			event.End = len(data) <= 32*1024
		}
		a.sequence++
		event.Sequence = a.sequence
		encoded, err := common.Marshal(event)
		if err == nil {
			if len(chunk) == 0 {
				_, err = a.writer.Write(append(encoded, '\n'))
			} else {
				_, err = a.writer.Write(encoded[:len(encoded)-1])
				if err == nil {
					_, err = a.writer.WriteString(",\"data_base64\":\"")
				}
				if err == nil {
					encoder := base64.NewEncoder(base64.StdEncoding, a.writer)
					_, err = encoder.Write(chunk)
					if closeErr := encoder.Close(); err == nil {
						err = closeErr
					}
				}
				if err == nil {
					_, err = a.writer.WriteString("\"}\n")
				}
			}
		}
		if err != nil {
			a.err = err
			return
		}
		if len(data) <= 32*1024 {
			break
		}
		event.Data = data[32*1024:]
	}
}

func (a *logDetailArchive) cleanup() {
	if a == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	if a.file != nil {
		if err := a.file.Close(); err != nil {
			logger.LogError(context.Background(), "close log detail spool: "+err.Error())
		}
		if err := os.Remove(a.file.Name()); err != nil {
			logger.LogError(context.Background(), "remove log detail spool: "+err.Error())
		}
	}
}

func archiveHeaders(headers http.Header) http.Header {
	copy := headers.Clone()
	for key := range copy {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "authorization") || strings.Contains(lower, "cookie") || strings.Contains(lower, "token") || strings.Contains(lower, "key") || strings.Contains(lower, "secret") || strings.Contains(lower, "password") {
			copy[key] = []string{"[REDACTED]"}
		} else if strings.EqualFold(key, "Location") {
			for i, value := range copy[key] {
				copy[key][i] = archiveURL(value)
			}
		}
	}
	return copy
}

func archiveURL(raw string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return "[invalid URL]"
	}
	parsed.User = nil
	return relaycommon.SanitizeURLForLog(parsed.String())
}

func startLogDetailArchive(c *gin.Context) *logDetailArchive {
	_, file, err := common.CreateDiskCacheFile(common.DiskCacheTypeLogDetail)
	a := &logDetailArchive{file: file, err: err}
	if err != nil {
		return a
	}
	a.writer = bufio.NewWriterSize(file, 32*1024)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), logDetailArchiveContextKey{}, a))
	a.record(logDetailArchiveEvent{Stage: "client_request", Method: c.Request.Method, URL: archiveURL(c.Request.URL.String()), Headers: archiveHeaders(c.Request.Header)})
	var storage common.BodyStorage
	if cached, exists := c.Get(common.KeyBodyStorage); exists {
		storage, _ = cached.(common.BodyStorage)
	}
	if storage == nil {
		storage, err = common.GetBodyStorage(c)
	}
	if err == nil {
		var reader io.ReadCloser
		reader, err = storage.NewReader()
		if err == nil {
			buffer := make([]byte, 32*1024)
			for {
				n, readErr := reader.Read(buffer)
				if n > 0 {
					a.record(logDetailArchiveEvent{Stage: "client_request_body", Data: buffer[:n]})
				}
				if readErr != nil {
					if readErr != io.EOF {
						err = readErr
					}
					break
				}
			}
			if closeErr := reader.Close(); err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		a.incomplete = true
		a.record(logDetailArchiveEvent{Stage: "client_request_body", Error: err.Error()})
	}
	a.record(logDetailArchiveEvent{Stage: "client_request_body", End: true})
	return a
}

type archiveBodyReader struct {
	io.ReadCloser
	archive  *logDetailArchive
	stage    string
	exchange int64
	replay   int64
	mu       sync.Mutex
	ended    bool
	expected int64
	seen     int64
}

func (a *logDetailArchive) body(reader io.ReadCloser, stage string, exchange, replay, expected int64) *archiveBodyReader {
	a.mu.Lock()
	a.pendingBodies++
	a.mu.Unlock()
	return &archiveBodyReader{ReadCloser: reader, archive: a, stage: stage, exchange: exchange, replay: replay, expected: expected}
}

func (r *archiveBodyReader) Read(p []byte) (int, error) {
	n, err := r.ReadCloser.Read(p)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen += int64(n)
	if n > 0 {
		r.archive.record(logDetailArchiveEvent{Stage: r.stage, Exchange: r.exchange, Replay: r.replay, Data: p[:n]})
	}
	if !r.ended && (err != nil || (r.expected >= 0 && r.seen >= r.expected)) {
		event := logDetailArchiveEvent{Stage: r.stage, Exchange: r.exchange, Replay: r.replay, End: true}
		r.archive.mu.Lock()
		r.archive.pendingBodies--
		if err != nil && err != io.EOF {
			event.Error = err.Error()
			r.archive.incomplete = true
		} else if err == io.EOF && r.expected >= 0 && r.seen < r.expected {
			event.Error = io.ErrUnexpectedEOF.Error()
			r.archive.incomplete = true
		}
		r.archive.mu.Unlock()
		r.archive.record(event)
		r.ended = true
	}
	return n, err
}

func (r *archiveBodyReader) Close() error {
	err := r.ReadCloser.Close()
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.ended {
		r.archive.mu.Lock()
		r.archive.incomplete = true
		r.archive.pendingBodies--
		r.archive.mu.Unlock()
		r.archive.record(logDetailArchiveEvent{Stage: r.stage, Exchange: r.exchange, Replay: r.replay, End: true, Error: "body closed before EOF"})
		r.ended = true
	}
	return err
}

type logDetailTransport struct{ http.RoundTripper }

func markLogDetailArchiveIncomplete(ctx context.Context, reason string) {
	if a, _ := ctx.Value(logDetailArchiveContextKey{}).(*logDetailArchive); a != nil {
		a.mu.Lock()
		a.incomplete = true
		a.mu.Unlock()
		a.record(logDetailArchiveEvent{Stage: "capture_incomplete", Error: reason})
	}
}

func RecordLogDetailWebSocketError(c *gin.Context, err error) {
	if c != nil && c.Request != nil && err != nil {
		markLogDetailArchiveIncomplete(c.Request.Context(), common.MaskSensitiveInfo(err.Error()))
	}
}

func (t *logDetailTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	a, _ := req.Context().Value(logDetailArchiveContextKey{}).(*logDetailArchive)
	if a == nil {
		return t.RoundTripper.RoundTrip(req)
	}
	a.mu.Lock()
	a.exchange++
	a.upstreamObserved = true
	exchange := a.exchange
	a.mu.Unlock()
	a.record(logDetailArchiveEvent{Stage: "upstream_request", Exchange: exchange, Method: req.Method, URL: archiveURL(req.URL.String()), Host: req.Host, Headers: archiveHeaders(req.Header)})
	// Clone 保留取消信号与 GetBody；不修改调用方复用的请求对象。
	request := req.Clone(req.Context())
	if req.Body != nil && req.Body != http.NoBody {
		expected := req.ContentLength
		if expected == 0 {
			expected = -1
		}
		request.Body = a.body(req.Body, "upstream_request_body", exchange, 0, expected)
	}
	var replay atomic.Int64
	if req.GetBody != nil {
		request.GetBody = func() (io.ReadCloser, error) {
			body, err := req.GetBody()
			if err != nil {
				return nil, err
			}
			attempt := replay.Add(1)
			a.record(logDetailArchiveEvent{Stage: "upstream_request_replay", Exchange: exchange, Replay: attempt})
			expected := req.ContentLength
			if expected == 0 {
				expected = -1
			}
			return a.body(body, "upstream_request_body", exchange, attempt, expected), nil
		}
	}
	resp, err := t.RoundTripper.RoundTrip(request)
	if err != nil {
		a.record(logDetailArchiveEvent{Stage: "upstream_error", Exchange: exchange, Error: common.MaskSensitiveInfo(err.Error())})
		return resp, err
	}
	a.record(logDetailArchiveEvent{Stage: "upstream_response", Exchange: exchange, Replay: replay.Load(), Status: resp.StatusCode, Protocol: resp.Proto, Headers: archiveHeaders(resp.Header)})
	if resp.Body != nil && resp.Body != http.NoBody {
		resp.Body = a.body(resp.Body, "upstream_response_body", exchange, replay.Load(), resp.ContentLength)
	} else {
		a.record(logDetailArchiveEvent{Stage: "upstream_response_body", Exchange: exchange, Replay: replay.Load(), End: true})
	}
	return resp, nil
}

func (t *logDetailTransport) CloseIdleConnections() { closeIdleConnections(t.RoundTripper) }

// AttachLogDetailRequestContext 给仍使用独立请求上下文的适配器传递采集器，不改取消语义。
func AttachLogDetailRequestContext(c *gin.Context, req *http.Request) *http.Request {
	if c == nil || c.Request == nil || req == nil {
		return req
	}
	a, _ := c.Request.Context().Value(logDetailArchiveContextKey{}).(*logDetailArchive)
	if a == nil {
		return req
	}
	return req.WithContext(context.WithValue(req.Context(), logDetailArchiveContextKey{}, a))
}

func RecordLogDetailWebSocket(c *gin.Context, stage string, messageType int, data []byte) {
	if capture := getLogDetailCapture(c); capture != nil && capture.archive != nil {
		if strings.HasPrefix(stage, "upstream_") {
			capture.archive.mu.Lock()
			capture.archive.upstreamObserved = true
			capture.archive.mu.Unlock()
		}
		capture.archive.record(logDetailArchiveEvent{Stage: stage, MessageType: messageType, Data: data})
	}
}

func RecordLogDetailWebSocketHandshake(c *gin.Context, targetURL string, headers http.Header, response *http.Response, err error) {
	if capture := getLogDetailCapture(c); capture != nil && capture.archive != nil {
		a := capture.archive
		a.mu.Lock()
		a.upstreamObserved = true
		a.mu.Unlock()
		a.record(logDetailArchiveEvent{Stage: "upstream_websocket_handshake", Method: http.MethodGet, URL: archiveURL(targetURL), Headers: archiveHeaders(headers)})
		if response != nil {
			a.record(logDetailArchiveEvent{Stage: "upstream_websocket_handshake_response", Status: response.StatusCode, Headers: archiveHeaders(response.Header)})
		}
		RecordLogDetailWebSocketError(c, err)
	}
}

func (a *logDetailArchive) finish(headers http.Header, status int) (io.Reader, bool, error) {
	a.mu.Lock()
	pending := a.pendingBodies
	a.mu.Unlock()
	if pending != 0 {
		a.record(logDetailArchiveEvent{Stage: "capture_incomplete", Error: "upstream bodies still open at request completion"})
	}
	a.record(logDetailArchiveEvent{Stage: "client_response", Headers: archiveHeaders(headers), Status: status, End: true})
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	if a.err != nil {
		return nil, false, fmt.Errorf("write log detail archive: %w", a.err)
	}
	if err := a.writer.Flush(); err != nil {
		return nil, false, err
	}
	_, err := a.file.Seek(0, io.SeekStart)
	return a.file, !a.incomplete && a.pendingBodies == 0 && err == nil, err
}
