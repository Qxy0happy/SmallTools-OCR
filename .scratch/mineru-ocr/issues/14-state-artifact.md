# 14 — 指纹、状态与产物

Type: task
Status: open
Blocked by: 13

规格：[`../spec.md`](../spec.md) §5、§7 第 5 步。
上游决策：[`04`](04-artifact-and-placement.md) [`06`](06-state-and-resume.md)。

## 要做什么

1. `internal/state`：
   - `type Error struct{ Code, Message string }`
   - `type FileRecord struct{ Size int64; Hash string; Tier, ZipPath, Status string; Error *Error; DurationMS int64; UpdatedAt time.Time }`
   - `type ServiceRecord struct{ PID, Port int; StartedAt time.Time }`
   - `type State struct{ SchemaVersion int; ToolVersion string; Files map[string]FileRecord; Service *ServiceRecord }`
   - `func Load(root string) (*State, error)`：文件不存在 → 空状态；JSON 损坏 → 把原文件改名 `.mineru-state.json.bak` 后返回空状态 + `ErrCorrupt`（调用方打警告，不中止）。
   - `func (s *State) Save(root string) error`：整体写 `*.tmp` 后 rename；并发安全（`sync.Mutex` 或调用方串行）。
   - `func (s *State) ShouldSkip(path, hash string) bool`：`status=="completed" && hash 一致 && zip 可打开且含 markdown.md`。
   - `func Fingerprint(path string) (string, int64, error)`：xxh3-128，32 位小写十六进制；返回 size。
   - `func StatePath(root string) string`：根是目录 → `<root>/.mineru-state.json`；根是文件 → `<dir(file)>/.mineru-state.json`。
2. `internal/artifact`：
   - `func ZipPathFor(src string) string` → `<src>.zip`。
   - `func Validate(path string) error`：`zip.OpenReader` 成功、至少一个条目、含 `markdown.md`。
   - `func Write(partPath, destPath string) error`：`os.Rename`（跨卷时 copy+rename）。

## 验收

- `go build ./... && go vet ./... && gofmt -l .` 无输出；`go test ./...` 全绿。
- 测试覆盖：指纹稳定且与已知向量一致（同内容两次相同、改一字节即不同）；`Save`/`Load` 往返；损坏 JSON → 生成 `.bak` 且返回 `ErrCorrupt`；`ShouldSkip` 的三种不满足（hash 不同 / zip 缺失 / zip 无 markdown.md）；`ZipPathFor` 同名不同扩展名不冲突；`Validate` 对空文件、非 zip、缺 `markdown.md` 的 zip 都报错。
- 不新增依赖。
