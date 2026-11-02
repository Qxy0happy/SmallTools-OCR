# 13 — 骨架、CLI 与发现

Type: task
Status: open
Blocked by: —

规格：[`../spec.md`](../spec.md) §2 §3 §4 §9 §10。
上游决策：[`01`](01-task-api-contract.md) [`05`](05-discovery-scope.md) [`09`](09-cli-surface-and-errors.md)。

## 要做什么

1. `go.mod`：`module mineru-ocr`，`go 1.27`。依赖只允许 `github.com/zeebo/xxh3`、`github.com/spf13/pflag`（`golang.org/x/sys` 仅作传递依赖）。
2. `internal/report`：
   - `type Error struct{ Code, Message string }`
   - `type FileStatus string`（`completed`/`skipped`/`failed`）
   - `type FileResult struct{ Path string; Status FileStatus; Error *Error; DurationMS int64; ZipPath string }`
   - `type ServiceInfo struct{ Port int; ReadyMS int64; Stopped bool }`
   - `type Report struct{ SchemaVersion int; Roots []string; Scanned, Skipped, Completed, Failed int; ExitCode int; Service ServiceInfo; Files []FileResult }`
   - `func (r *Report) Add(FileResult)`；`func Summary(w io.Writer, r *Report)`（人读汇总表）；`func WriteJSON(w io.Writer, r *Report) error`
   - `type Progress struct{ Out io.Writer; Total int }` + `func (p *Progress) Line(i int, fr FileResult)`：`[3/42] ok   report.pdf  12.3s` / `[4/42] FAIL scan.docx  parse_failed: <msg>`
3. `internal/app`：`type Config struct{ Roots []string; Workers int; Tier, Project string; Port int; DryRun, JSON, Verbose, NoServiceLogStream bool; ServiceLog string; Out, Err io.Writer }`；`func Parse(args []string) (Config, error)`（pflag，允许 flag 与位置参数交错；无根 → error）；`func Defaults() Config`（`Workers=2`、`Tier=advanced`、`Project=$MINERU_PROJECT`→`D:/Projects/Python/MinerU`、`Port=0`）。
4. `cmd/mineru-ocr/main.go`：解析 → 调 `run.Run(ctx, cfg)` → `os.Exit(code)`；`--help` 退出 0；解析失败打印用法到 stderr 并退出 2。
5. `internal/discover`：`func Find(roots []string, warn func(string)) ([]string, error)`
   - 扩展名 `.pdf .doc .docx .ppt .pptx`，大小写不敏感；根可为文件；
   - 跳过：以 `.` 开头的目录、符号链接（目录与文件）、不可读目录（`warn` 后继续）；
   - 绝对路径化、根去重、跨根按绝对路径去重、结果排序。
6. `internal/run`：本票只实现 `Run` 的**扫描 + `--dry-run`** 路径（启动服务、处理文件由票据 17 接上）：
   - `--dry-run`：打印计划（每文件一行 `would process <path>` 到 stderr），退出 0；不写状态、不启服务。
   - 无候选文件：stdout 输出空汇总，退出 0。
7. `--json` 在本票至少让 `--dry-run` 输出合法 JSON 报告。

## 验收

- `go build ./... && go vet ./... && gofmt -l .`（无输出）。
- `go test ./...` 覆盖：flag 默认值与优先级（flag > env > 默认）、位置参数与 flag 交错、发现规则（隐藏目录/符号链接/大小写/根为文件/去重/排序）。
- `go run ./cmd/mineru-ocr --help` 列出规格 §2 的全部 flag。
- `go run ./cmd/mineru-ocr`（无参数）退出 2。
- 在临时目录造 3 个文件（`.pdf`/`.DOCX`/`a.txt`）后 `go run ./cmd/mineru-ocr <dir> --dry-run` 只列出 2 个，退出 0。
- `--dry-run --json` 输出可被 `encoding/json` 解析且字段符合 spec §3。

## 边界

非 `--dry-run` 路径在票据 17 之前可以返回明确的“尚未实现”错误（退出 2）；票据 17 必须把它替换成真实流水线，最终交付不得留下占位实现。
