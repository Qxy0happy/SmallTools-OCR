# mineru-ocr 规格

状态：可实施（hash 已锁）。术语见 [`../../GLOSSARY.md`](../../GLOSSARY.md)。决策出处见 [`map.md`](map.md)。

## 1. 范围

Go 单命令工具：递归发现 PDF 与 Office 文档，逐个交给**本程序自己启动**的本地 MinerU 服务以 `advanced` 档解析，产物 zip 落在源文件同目录，并以每根一份状态文件支持增量跳过与有限重试。

非目标：解压产物、直出 markdown、常驻 watch、远程服务、外接已有实例、MinerU 其余扩展名与图片 OCR。

## 2. CLI

```
mineru-ocr <root>... [flags]
```

| flag | 默认 | 说明 |
|---|---|---|
| `--workers` | `2` | 客户端并发；同时作为服务端 `--concurrency` |
| `--tier` | `advanced` | 只影响 PDF；透传给服务端启动参数 |
| `--mineru-project` | `$MINERU_PROJECT` → `D:/Projects/Python/MinerU` | `uv run --project` 的目标 |
| `--port` | `0` | `0` = 自动挑空闲端口 |
| `--dry-run` | 关 | 只扫描并打印计划，不启动服务、不写状态 |
| `--json` | 关 | stdout 输出机器可读报告 |
| `--verbose` | 关 | 打印每步细节（含轮询与重试） |
| `--service-log` | `<root>/.mineru-service.log` | 服务端 stdout+stderr 落盘；每次运行覆盖 |
| `--no-service-log-stream` | 关 | 关闭服务日志的实时透传（文件照写） |

配置优先级：**flag > 环境变量 > 内置默认**。无配置文件，无 `--base-url`。

## 3. 退出码与输出流

| 退出码 | 含义 |
|---|---|
| `0` | 全部成功，或无事可做 |
| `1` | 至少一个文件失败 |
| `2` | 致命：参数非法、服务启动/就绪失败、运行中服务失联、端口无法分配 |
| `130` | 用户中断（Ctrl+C） |

- 进度与错误 → **stderr**，每文件一行：
  `[3/42] ok   report.pdf  12.3s`
  `[4/42] FAIL scan.docx  parse_failed: ...`
- **服务端日志全程实时透传到 stderr**，逐行加 `[service] ` 前缀（启动、模型加载、就绪、运行期、关停均可见），同时原样写入 `--service-log`；`--no-service-log-stream` 只关透传。
- 汇总表 → **stdout**（`--json` 时改为单个 JSON 对象）。
- 服务日志始终落 `--service-log`，失败时 stderr 追加其最后 50 行。

`--json` 形状：

```json
{
  "schema_version": 1,
  "roots": ["D:/docs"],
  "scanned": 12, "skipped": 3, "completed": 8, "failed": 1,
  "exit_code": 1,
  "service": {"port": 54321, "ready_ms": 28010, "stopped": true},
  "files": [
    {"path": "D:/docs/a.pdf", "status": "completed", "zip_path": "D:/docs/a.pdf.zip", "duration_ms": 12340},
    {"path": "D:/docs/b.docx", "status": "failed", "error": {"code": "parse_failed", "message": "..."}}
  ]
}
```

## 4. 发现

- 扩展名固定：`.pdf .doc .docx .ppt .pptx`，大小写不敏感；不可通过 flag 扩展。
- 根可以是目录或文件；相对路径转绝对路径；重复根去重。
- 跳过：目录名以 `.` 开头（含 `.git`）、符号链接（目录与文件都不跟随）、不可读目录（stderr 警告后继续）。
- 候选按绝对路径排序后进入 worker 池；跨根按绝对路径去重（先出现的根胜出）。

## 5. 指纹与状态

- 指纹：**xxh3-128**（`github.com/zeebo/xxh3`），对文件全量读取，32 位十六进制小写。
- 状态文件：每个根下 `.mineru-state.json`；根是文件时放在其所在目录。多根共享嵌套路径时，记录写入“先出现的根”的状态文件。

```json
{
  "schema_version": 1,
  "tool_version": "0.1.0",
  "files": {
    "D:/docs/a.pdf": {
      "size": 123456, "hash": "<32 hex>", "tier": "advanced",
      "zip_path": "D:/docs/a.pdf.zip", "status": "completed",
      "error": null, "duration_ms": 12340, "updated_at": "2026-11-02T20:00:00Z"
    }
  },
  "service": {"pid": 4321, "port": 54321, "started_at": "2026-11-02T19:59:30Z"}
}
```

- 跳过条件（三条全满足）：`status == "completed"`、`hash` 一致、`zip_path` 存在且能以 zip 打开并含 `markdown.md`。任一不满足 → 重做并覆盖。
- 只持久化终态；不存 in-flight / task_id。
- 写入：每文件完成后整体原子重写（`*.tmp` + rename）。
- 损坏：JSON 解析失败 → 备份为 `.mineru-state.json.bak`，以空状态继续，stderr 警告，退出码不受影响。
- 文件在指纹计算与上传之间变化（size 不符）→ 该文件失败，错误码 `source_changed`，状态不记 completed。

## 6. 服务编排

### 6.1 启动前清理残留

对每个根的 `service` 记录，**三条同时满足**才认定为自家残留：pid 存活、记录端口 `GET /v1/health` 返回 200、该 pid 的 `Win32_Process.CommandLine` 含 `mineru-kit`、`api-server` 与 `--port <记录端口>`。命中则按 6.4 关停；任一不满足则 stderr 警告，不杀。

### 6.2 端口与启动

0. 启动前 `exec.LookPath("uv")`；缺失则退出 2，提示安装 uv（https://docs.astral.sh/uv/）。所有 Python 侧执行（服务、模型下载、e2e）一律经 `uv run --project`，禁止直接调用系统 `python`/`pip`。
1. `net.Listen("tcp", "127.0.0.1:0")` 取端口后立即关闭；`--port` 非 0 时以其为准。
2. 启动：
   ```
   uv run --project <project> mineru-kit api-server \
     --port <port> --upload-dir <temp>/mineru-ocr-<port> \
     --tier <tier> --concurrency <workers> --preload-models
   ```
   `SysProcAttr{CreationFlags: CREATE_NEW_PROCESS_GROUP}`；stdout/stderr 同时写入 `--service-log`（原样）和 stderr（逐行 `[service] ` 前缀，实时）。
3. 启动瞬间把 `service{pid, port, started_at}` 写入各根状态文件。

### 6.3 就绪与存活

- 就绪：每 250ms `GET /v1/health`（单次 1s 超时），200 即就绪；**连接被拒也算未就绪**，不要等 503。
- 就绪超时 **120s**（preload 实测 28.01s）；期间进程退出 → 读日志尾部：
  - 退出码 `3` 或含 `WinError 10048` → 换新端口重试**一次**；仍失败 → 退出 2。
  - 退出码 `1` 且含 `Unsupported tier` → 直接退出 2（`--tier` 传给服务端的值必须合法）。
  - 其他 → stderr 提示 `uv run --project <project> mineru-kit models download`，退出 2。
- 存活：运行中每 5s 探 `GET /v1/health`；连续 3 次失败或进程退出 → 停止派发，等在飞任务 ≤30s，汇总后退出 2。

### 6.4 关停

`GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, uv_pid)` → 等 5s → 未退则 `taskkill /T /F /PID <uv_pid>`；随后确认端口不再可连（≤5s）。正常结束、错误结束、Ctrl+C 都走同一序列。

## 7. 每文件处理

1. 计算指纹；按 §5 决定跳过。
2. `POST /v1/tasks`（超时 120s）：
   - PDF：`{"files":[{"file_name":"<name>","bytes":<size>}],"tier":"advanced","ocr_mode":"auto","page_range":"","output_formats":["default"]}`
   - Office：同上但**不带 `tier`**（服务端强制 flash）。
   - `202` → 取 `files[0].upload.url`。
3. `PUT <upload.url>`，`application/octet-stream`，`Content-Length` = 声明 `bytes`；上传不设总超时。
4. 轮询 `GET /v1/tasks/{id}`（单次 30s）：间隔 1s，任务超过 60s 未终态后放宽到 5s；不设总时限。
5. 终态：
   - `completed` → `GET <result_url>`（超时 300s，最多 5 次重定向）写 `<zip_path>.part` → `zip.OpenReader` 成功且含 `markdown.md` → rename。校验失败记 `invalid_artifact`。
   - `failed` / `canceled` → 取 `files[0].error{code,message}`，无则由状态兜底。
   - 任务 404 → `task_not_found`。
6. 写状态（原子）并打印进度行。

重试：仅网络类错误（连接失败、408/429/5xx、上传中断）重试，每步骤最多 3 次，退避 0.25s/0.5s/1s。`400/401/403/404/413` 与 zip 校验失败不重试。

Ctrl+C：对在飞 task 发 `POST /v1/tasks/{id}/cancel`（5s 超时）→ 关停服务 → 退出 130；状态不记 completed。

## 8. 并发

worker 池大小 = `--workers`；每 worker 同一时刻推进一个文件；服务端 `--concurrency` 同值。worker 之间不共享任务状态，只共享只读配置与写的串行化状态文件（互斥锁）。

## 9. 实现约束

- Go 1.27+，Windows 目标；非 Windows 直接退出 2（本规格只覆盖 Windows 进程控制）。
- 依赖仅：`github.com/zeebo/xxh3`、`github.com/spf13/pflag`。
- Python 环境全部由 uv 管理与运行：子进程固定为 `uv run --project <project> mineru-kit ...`，代码中不出现裸 `python`/`pip` 调用。
- 建议包结构：`cmd/mineru-ocr`、`internal/discover`、`internal/state`、`internal/mineru`（HTTP 客户端）、`internal/service`（进程监管）、`internal/run`（编排）、`internal/report`。

## 10. 验收标准

- `go test ./...` 全绿；集成测试用 `httptest` 假服务，不依赖 GPU。
- `go run . <fixture> --dry-run`：只打印计划，无 uv 进程，无状态文件。
- 首次 `go run . <fixture>`：生成 `<basename>.<ext>.zip`，退出 0，状态记 completed。
- 第二次运行：全部跳过，退出 0。
- 删除其中一个 zip 后重跑：该文件重做，其余跳过。
- `--mineru-project` 指向空目录：退出 2，stderr 含日志尾部与 `uv run --project <project> mineru-kit models download` 提示。
- PATH 中没有 `uv`：退出 2，提示安装 uv，且不启动任何服务。
- 运行中 Ctrl+C：在飞任务取消、服务退出、端口释放、无 `uv`/`python` 残留、退出 130。
- 启动期间 stderr 实时出现 `[service]` 前缀日志（可见 `Initializing VLM client`、`Application startup complete.`、`Uvicorn running on http://127.0.0.1:<port>`），且与 `--service-log` 文件内容一致；`--no-service-log-stream` 时 stderr 无 `[service]` 行而文件仍有内容。
- 手工制造残留（`taskkill /F` 杀宿主）后重跑：按 §6.1 清理旧实例并正常完成。

## 11. 已接受风险

- `--tier advanced` 由本程序保证；若服务端拒绝，逐文件 400 原样上报，不降级。
- 自动端口 `bind→close→传参` 有极小竞态，靠“启动失败换端口重试一次”兜底。
- 残留识别依赖命令行匹配；`Win32_Process.CommandLine` 取不到时按“不杀 + 警告”处理。
