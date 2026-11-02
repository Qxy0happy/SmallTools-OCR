# 15 — MinerU HTTP 客户端

Type: task
Status: resolved
Blocked by: 13

规格：[`../spec.md`](../spec.md) §7。
上游决策：[`01`](01-task-api-contract.md) [`03`](03-tier-policy.md)。

## 要做什么

`internal/mineru`，只依赖 `net/http` 与标准库：

```go
type Client struct{ BaseURL string; HTTP *http.Client }
type FileDetail struct{ Status string; ResultURL string; Error *Error }
func New(baseURL string) *Client
func (c *Client) Health(ctx context.Context) error                       // GET /v1/health，200 视为就绪
func (c *Client) Create(ctx context.Context, name string, size int64, tier string) (taskID, uploadURL string, err error)
func (c *Client) Upload(ctx context.Context, uploadURL, srcPath string) error
func (c *Client) Poll(ctx context.Context, taskID string) (status string, file FileDetail, err error)
func (c *Client) Download(ctx context.Context, resultURL, partPath string) error
func (c *Client) Cancel(ctx context.Context, taskID string) error        // 5s 超时
```

约束：

- `Create` 请求体：`{"files":[{"file_name","bytes"}],"ocr_mode":"auto","page_range":"","output_formats":["default"]}`；`tier` 非空时才写入 `"tier"`（PDF 传 `advanced`，Office 传空串）。
- `Upload`：`application/octet-stream`，`Content-Length` = 文件实际大小；校验请求体大小与 `Create` 声明一致，不一致返回 `ErrSourceChanged`。
- `Poll`：状态机解析 `status`、`files[0].result_url`、`files[0].error`；`completed`/`failed`/`canceled` 为终态。
- 错误分类：`type Error struct{ Code, Message string; Status int; Retryable bool }`；`400/401/403/404/413` 不可重试；`408/429/500/502/503/504` 与连接失败可重试。
- 重试：`Retry(ctx, fn)` 辅助，最多 3 次，退避 0.25s/0.5s/1s（可注入 sleep 便于测试）。
- 超时：`Create` 120s、`Poll` 单次 30s、`Download` 300s（最多 5 次重定向）、`Health` 1s。

## 验收

- `httptest` 覆盖：PDF 请求含 `tier=advanced`、Office 请求无 `tier`；`Content-Length` 等于文件大小；202 → upload → 200；轮询 `running`→`completed`；`failed` 时错误码透出；500 重试 2 次后成功；400 不重试；下载写到临时文件；`Health` 在 500 时返回错误。
- `go build ./... && go vet ./... && gofmt -l . && go test ./...` 全绿；不新增依赖。

## Answer

已实现 `internal/mineru`（Health/Create/Upload/Poll/Download/Cancel、`Error{Code,Message,Status,Retryable}`、`Retry` 0.25/0.5/1s、`ErrSourceChanged`、重定向与超时）。测试 `internal/mineru/client_test.go` 12 个用例（PDF 带 tier、Office 不带、Content-Length、轮询终态、failed 透传、500 重试、400 不重试、下载、重定向、Health）。
