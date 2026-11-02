# 16 — 服务监管（启动/就绪/关停/残留）

Type: task
Status: open
Blocked by: 13

规格：[`../spec.md`](../spec.md) §6。
上游决策：[`07`](07-service-lifecycle.md) [`08`](08-windows-process-control.md) [`11`](11-residual-service-cleanup.md)。
实测：[`../research/07-service-lifecycle.md`](../research/07-service-lifecycle.md)、[`../research/08-windows-process-control.md`](../research/08-windows-process-control.md)。

## 要做什么

`internal/service`：

```go
type Options struct{ Project, Tier string; Workers, Port int; LogPath string; LogStream io.Writer }
type Supervisor interface {
    Start(ctx context.Context) (report.ServiceInfo, error)
    Health(ctx context.Context) error
    Stop(ctx context.Context) error
}
func New(opts Options) Supervisor
```

- **端口**：`Port==0` 时 `net.Listen("tcp","127.0.0.1:0")` 取端口再关闭。
- **启动命令**：`uv run --project <Project> mineru-kit api-server --port <port> --tier <Tier> --concurrency <Workers> --preload-models`；`SysProcAttr.CreationFlags = CREATE_NEW_PROCESS_GROUP`。
- **日志双写**：stdout/stderr 用 `io.MultiWriter(file, lineWriter{prefix:"[service] ", w:LogStream})`；`LogStream==nil` 时只写文件。逐行加前缀、实时 flush、并发安全。
- **就绪**：每 250ms `GET /v1/health`（1s 超时），连接被拒算未就绪；总超时 **120s**；进程先退出 → 读日志尾部 50 行，退出码 `3` 或含 `WinError 10048` → 换端口重试一次；退出码 `1` 且含 `Unsupported tier` → 不可重试；其余带 `mineru-kit models download` 提示。
- **存活监控**：`Health` 每 5s 被调用一次（由 orchestrator 驱动）；连续 3 次失败即判定失联。
- **关停**：`GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, uvPID)` → 等 5s → `taskkill /T /F /PID <uvPID>`；随后确认端口不可连（≤5s）。
- **残留清理** `CleanupStale(records []Record) error`，其中 `type Record struct{ PID, Port int }`（不依赖 state 包）：pid 存活 + 端口 health 200 + `Get-CimInstance Win32_Process` 的 CommandLine 同时含 `mineru-kit`、`api-server`、`--port <port>` 三条同时成立才关停；否则警告不杀。
- Windows 专用：非 Windows 返回 `ErrUnsupportedPlatform`。Win32 调用用 `syscall` 的 `NewLazyDLL`，不新增依赖。

## 验收

- 单测（无真实进程）：命令参数拼装、空闲端口、日志前缀 writer（多行/半行/并发）、就绪轮询（`httptest` 延迟 200）、就绪超时、退出码 3 换端口重试一次、`Unsupported tier` 不重试、`CleanupStale` 三条判据的真值表（用注入的 fake `pidAlive`/`health`/`commandLine`）。
- 进程级测试放 `-tags=e2e`，默认不跑。
- `go build ./... && go vet ./... && gofmt -l . && go test ./...` 全绿；不新增依赖。
