package mineru

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

var ErrSourceChanged = errors.New("mineru: source file changed")

// Sleep is the default sleeper used by Retry when no sleeper is injected.
var Sleep = time.Sleep

type Client struct {
	BaseURL string
	HTTP    *http.Client

	declaredMu sync.Mutex
	declared   map[string]int64
}

type Error struct {
	Code      string
	Message   string
	Status    int
	Retryable bool
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	if e.Message != "" {
		return fmt.Sprintf("mineru: %s (status %d): %s", e.Code, e.Status, e.Message)
	}
	return fmt.Sprintf("mineru: %s (status %d)", e.Code, e.Status)
}

type FileDetail struct {
	Status    string
	ResultURL string
	Error     *Error
}

func New(baseURL string) *Client {
	return &Client{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		HTTP:     &http.Client{},
		declared: map[string]int64{},
	}
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{}
}

func (c *Client) url(path string) string {
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return c.BaseURL + path
}

func classifyStatus(status int) bool {
	switch status {
	case 400, 401, 403, 404, 413:
		return false
	case 408, 429, 500, 502, 503, 504:
		return true
	}
	if status >= 500 {
		return true
	}
	return false
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func parseError(status int, body []byte) *Error {
	e := &Error{Status: status, Retryable: classifyStatus(status)}
	var eb errorBody
	if json.Unmarshal(body, &eb) == nil {
		e.Code = eb.Error.Code
		e.Message = eb.Error.Message
	}
	if e.Code == "" {
		e.Code = fmt.Sprintf("http_%d", status)
	}
	return e
}

func retryableErr(err error) bool {
	if err == nil {
		return false
	}
	var e *Error
	if errors.As(err, &e) {
		return e.Retryable
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	return true
}

func Retry(ctx context.Context, fn func() error, sleep ...func(time.Duration)) error {
	sleeper := Sleep
	if len(sleep) > 0 && sleep[0] != nil {
		sleeper = sleep[0]
	}
	backoffs := []time.Duration{
		250 * time.Millisecond,
		500 * time.Millisecond,
		1 * time.Second,
	}
	var last error
	for attempt := range 4 {
		if err := ctx.Err(); err != nil {
			if last != nil {
				return last
			}
			return err
		}
		last = fn()
		if last == nil {
			return nil
		}
		if !retryableErr(last) {
			return last
		}
		if attempt < len(backoffs) {
			sleeper(backoffs[attempt])
		}
	}
	return last
}

// ---------- Health ----------

func (c *Client) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 1*time.Second)
	defer cancel()

	fn := func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/v1/health"), nil)
		if err != nil {
			return &Error{Code: "request_failed", Message: err.Error(), Status: 0, Retryable: true}
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return &Error{Code: "connection_error", Message: err.Error(), Status: 0, Retryable: true}
		}
		defer resp.Body.Close()
		io.Copy(io.Discard, resp.Body)
		if resp.StatusCode != http.StatusOK {
			return parseError(resp.StatusCode, nil)
		}
		return nil
	}
	// Health itself should not retry? spec says health ready; but classification allows. Keep no retry?
	// Acceptance: Health on 500 returns error. Use direct call to avoid delay.
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/v1/health"), nil)
	if err != nil {
		return &Error{Code: "request_failed", Message: err.Error(), Status: 0, Retryable: true}
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return &Error{Code: "connection_error", Message: err.Error(), Status: 0, Retryable: true}
	}
	if resp.StatusCode != http.StatusOK {
		return parseError(resp.StatusCode, nil)
	}
	_ = fn
	return nil
}

// ---------- Create ----------

type createFile struct {
	FileName string `json:"file_name"`
	Bytes    int64  `json:"bytes"`
}

type createRequest struct {
	Files         []createFile `json:"files"`
	OCRMode       string       `json:"ocr_mode"`
	PageRange     string       `json:"page_range"`
	OutputFormats []string     `json:"output_formats"`
	Tier          string       `json:"tier,omitempty"`
}

type createUpload struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

type createRespFile struct {
	FileID string        `json:"file_id"`
	Status string        `json:"status"`
	Upload *createUpload `json:"upload"`
}

type createResponse struct {
	TaskID string           `json:"task_id"`
	Status string           `json:"status"`
	Files  []createRespFile `json:"files"`
}

func (c *Client) Create(ctx context.Context, name string, size int64, tier string) (string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	var taskID, uploadURL string
	err := Retry(ctx, func() error {
		body := createRequest{
			Files:         []createFile{{FileName: name, Bytes: size}},
			OCRMode:       "auto",
			PageRange:     "",
			OutputFormats: []string{"default"},
		}
		if tier != "" {
			body.Tier = tier
		}
		b, err := json.Marshal(body)
		if err != nil {
			return &Error{Code: "marshal_error", Message: err.Error(), Status: 0, Retryable: false}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/v1/tasks"), bytes.NewReader(b))
		if err != nil {
			return &Error{Code: "request_failed", Message: err.Error(), Status: 0, Retryable: true}
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return &Error{Code: "connection_error", Message: err.Error(), Status: 0, Retryable: true}
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return parseError(resp.StatusCode, raw)
		}
		var cr createResponse
		if err := json.Unmarshal(raw, &cr); err != nil {
			return &Error{Code: "decode_error", Message: err.Error(), Status: resp.StatusCode, Retryable: false}
		}
		if len(cr.Files) == 0 || cr.Files[0].Upload == nil {
			return &Error{Code: "invalid_response", Message: "missing upload url", Status: resp.StatusCode, Retryable: false}
		}
		taskID = cr.TaskID
		uploadURL = cr.Files[0].Upload.URL
		if !strings.HasPrefix(uploadURL, "http") {
			uploadURL = c.url(uploadURL)
		}
		c.declaredMu.Lock()
		c.declared[uploadURL] = size
		c.declaredMu.Unlock()
		return nil
	})
	if err != nil {
		return "", "", err
	}
	return taskID, uploadURL, nil
}

// ---------- Upload ----------

func (c *Client) Upload(ctx context.Context, uploadURL, srcPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	info, err := os.Stat(srcPath)
	if err != nil {
		return &Error{Code: "source_error", Message: err.Error(), Status: 0, Retryable: false}
	}
	c.declaredMu.Lock()
	declared, hasDeclared := c.declared[uploadURL]
	c.declaredMu.Unlock()
	if hasDeclared && declared != info.Size() {
		return ErrSourceChanged
	}

	return Retry(ctx, func() error {
		f, err := os.Open(srcPath)
		if err != nil {
			return &Error{Code: "source_error", Message: err.Error(), Status: 0, Retryable: false}
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			return &Error{Code: "source_error", Message: err.Error(), Status: 0, Retryable: false}
		}
		if hasDeclared && fi.Size() != declared {
			return ErrSourceChanged
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.url(uploadURL), f)
		if err != nil {
			return &Error{Code: "request_failed", Message: err.Error(), Status: 0, Retryable: true}
		}
		req.Header.Set("Content-Type", "application/octet-stream")
		req.ContentLength = fi.Size()
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return &Error{Code: "connection_error", Message: err.Error(), Status: 0, Retryable: true}
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return parseError(resp.StatusCode, raw)
		}
		return nil
	})
}

// ---------- Poll ----------

type pollError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type pollFile struct {
	Status    string     `json:"status"`
	ResultURL string     `json:"result_url"`
	Error     *pollError `json:"error"`
}

type pollResponse struct {
	Status string     `json:"status"`
	Files  []pollFile `json:"files"`
}

func (c *Client) Poll(ctx context.Context, taskID string) (string, FileDetail, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	var status string
	var file FileDetail
	err := Retry(ctx, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/v1/tasks/"+taskID), nil)
		if err != nil {
			return &Error{Code: "request_failed", Message: err.Error(), Status: 0, Retryable: true}
		}
		resp, err := c.httpClient().Do(req)
		if err != nil {
			return &Error{Code: "connection_error", Message: err.Error(), Status: 0, Retryable: true}
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return parseError(resp.StatusCode, raw)
		}
		var pr pollResponse
		if err := json.Unmarshal(raw, &pr); err != nil {
			return &Error{Code: "decode_error", Message: err.Error(), Status: resp.StatusCode, Retryable: false}
		}
		status = pr.Status
		if len(pr.Files) > 0 {
			file.Status = pr.Files[0].Status
			file.ResultURL = pr.Files[0].ResultURL
			if pr.Files[0].Error != nil {
				file.Error = &Error{
					Code:      pr.Files[0].Error.Code,
					Message:   pr.Files[0].Error.Message,
					Status:    0,
					Retryable: false,
				}
			} else {
				file.Error = nil
			}
		}
		return nil
	})
	if err != nil {
		return "", FileDetail{}, err
	}
	return status, file, nil
}

// ---------- Download ----------

func (c *Client) Download(ctx context.Context, resultURL, partPath string) error {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	client := *c.httpClient()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		return nil
	}

	return Retry(ctx, func() error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(resultURL), nil)
		if err != nil {
			return &Error{Code: "request_failed", Message: err.Error(), Status: 0, Retryable: true}
		}
		resp, err := client.Do(req)
		if err != nil {
			return &Error{Code: "connection_error", Message: err.Error(), Status: 0, Retryable: true}
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			raw, _ := io.ReadAll(resp.Body)
			return parseError(resp.StatusCode, raw)
		}
		f, err := os.Create(partPath)
		if err != nil {
			return &Error{Code: "file_error", Message: err.Error(), Status: 0, Retryable: false}
		}
		_, copyErr := io.Copy(f, resp.Body)
		closeErr := f.Close()
		if copyErr != nil {
			return &Error{Code: "io_error", Message: copyErr.Error(), Status: 0, Retryable: true}
		}
		if closeErr != nil {
			return &Error{Code: "io_error", Message: closeErr.Error(), Status: 0, Retryable: true}
		}
		return nil
	})
}

// ---------- Cancel ----------

func (c *Client) Cancel(ctx context.Context, taskID string) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/v1/tasks/"+taskID+"/cancel"), nil)
	if err != nil {
		return &Error{Code: "request_failed", Message: err.Error(), Status: 0, Retryable: true}
	}
	resp, err := c.httpClient().Do(req)
	if err != nil {
		return &Error{Code: "connection_error", Message: err.Error(), Status: 0, Retryable: true}
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return parseError(resp.StatusCode, nil)
	}
	return nil
}
