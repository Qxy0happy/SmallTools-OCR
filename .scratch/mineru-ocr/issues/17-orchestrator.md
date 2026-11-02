# 17 — 编排：worker 池、流水线、报告、退出码

Type: task
Status: open
Blocked by: 13, 14, 15, 16

规格：[`../spec.md`](../spec.md) §3 §6.3 §7 §8。
上游决策：[`02`](02-per-file-task-concurrency.md) [`06`](06-state-and-resume.md) [`09`](09-cli-surface-and-errors.md)。

## 要做什么

补全 `internal/run`：

- `func Run(ctx context.Context, cfg app.Config) int`
- **顺序**：解析根 → 每根 `state.Load` → `discover.Find` → 指纹 → `ShouldSkip` → 需要处理的进 channel → `workers` 个 goroutine → 汇总。
- **每文件流水线**（§7）：`mineru.Create`（PDF 带 `advanced`，Office 不带）→ `Upload` → `Poll`（1s，>60s 后 5s）→ `Download` 到 `<zip>.part` → `artifact.Validate` → `artifact.Write` → 更新状态（原子、互斥）。
- **服务**：`service.Supervisor` 作为接口注入，`Run` 在 dry-run 之外启动服务；`ReadyMS` 写入报告；运行期每 5s `Health`，连续 3 次失败 → 停止派发、等在飞任务 ≤30s、退出 2。
- **Ctrl+C**：`signal.NotifyContext`；取消在飞任务（`Cancel`，5s）→ `Stop` → 退出 130。
- **输出**：进度行到 stderr（`report.Progress`）、汇总到 stdout、`--json` 时 stdout 只输出 JSON。
- **诊断**：服务日志尾部 50 行只在启动失败时追加；`Verbose` 打印轮询与重试。

可测性：`Run` 接受注入的 `mineru.Client` 构造器与 `service.Supervisor`（`type Deps struct{ NewClient func(baseURL string) ...; NewSupervisor func(...) ... }`），测试用 `httptest` 假 Tasks 服务 + 假 supervisor。

## 验收

- 集成测试（默认跑，无 GPU/无 uv）：假 supervisor（health 恒 200）+ `httptest` 实现 Tasks 协议：
  - 三个文件全成功 → 退出 0、3 个 zip 落地、状态 completed；
  - 第二次运行全跳过、不发起 HTTP；
  - 删除一个 zip → 只重做该文件；
  - 服务端对某文件返回 400 → 该文件 failed、其余成功、退出 1；
  - 500 两次后成功 → 重试生效；
  - `--json` 输出符合 spec §3 且 `exit_code` 与进程退出码一致；
  - 假 supervisor `Health` 连续失败 → 退出 2；
  - 取消上下文 → 在飞任务收到 `Cancel`、`Stop` 被调用、返回 130。
- 并发：`-race` 下无数据竞争。
- `go build ./... && go vet ./... && gofmt -l . && go test -race ./...` 全绿。
