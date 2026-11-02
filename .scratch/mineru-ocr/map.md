# MinerU 批量 OCR 工具 — Wayfinding Map

## Destination

一份可直接实现的规格（ticket 10 写入 `.scratch/mineru-ocr/spec.md`）：Go 单命令工具 `mineru-ocr <root>...` 自行启动本地 MinerU 服务（`uv run --project <MinerU> mineru-kit api-server`），递归发现 PDF 与 doc/docx/ppt/pptx，逐文件以 advanced 档解析，把 `result.zip` 原子落在源文件同目录，靠 `.mineru-state.json` 做增量跳过与有限重试。规格覆盖 CLI 契约、HTTP 交互、state schema、服务编排、失败语义与验收标准。

## Notes

- 领域事实来源：`D:/Projects/Python/MinerU` 源码（契约在 `mineru/tasks/`、`mineru/parser/`、`mineru/kit/`）；本机 Go 1.27.3、uv 0.13.6。
- 本仓库无 VCS（无 `.git`/`.jj`）：research 产物写 `.scratch/mineru-ocr/research/`，不走 `research/*` 分支。
- 本 effort 在 Notes 中覆写「只计划不执行」：交付物就是一份规格，由 ticket 10 写入，随 map 收尾。
- 已接受风险：
  - **tier**：由本程序以 `--tier advanced` 启动服务来保证；服务端若拒绝，逐文件 400 原样透出，不静默降级。
  - **端口**：bind→close→传参存在极小竞态；启动失败重试一次换端口。
  - **残留清理**：按 state 记录的 pid + 端口健康检查清理；pid 复用的极端情况可能误杀其他进程（记警告）。
  - **服务端限制不构成设计约束**：单任务 ≤100 文件、上传链接 1h、终态保留 86400s。
- 用户委托我决定的细节（原话「你自己看着搞吧」）：CLI 形状、配置优先级、退出码、stdout/stderr 分流、超时策略。
- 固定常量：`output_formats=["default"]`；2 workers；内容指纹 xxh3-128；扩展名固定 5 种。

## Decisions so far

- [对接的服务与协议](issues/01-task-api-contract.md)：走 `api-server` 的 Tasks 协议（create → PUT 上传 → 轮询 → 下载 zip），每文件一条任务。
- [任务粒度与并发](issues/02-per-file-task-concurrency.md)：每文件一个 task；客户端 2 workers；服务端 `--concurrency` 与 workers 同值。
- [档位策略](issues/03-tier-policy.md)：PDF 固定 advanced（启动参数保证）；Office 四类服务端强制 flash，请求不带 tier；不降级。
- [产物形态与落点](issues/04-artifact-and-placement.md)：`output_formats=["default"]`；zip 原样存 `<basename>.<ext>.zip` 于源文件同目录；`.part` 校验后原子 rename。
- [文件发现范围](issues/05-discovery-scope.md)：固定 5 种扩展名、大小写不敏感；跳过隐藏目录、符号链接；多根、排序稳定。
- [状态文件与增量](issues/06-state-and-resume.md)：根下 `.mineru-state.json`；xxh3-128 全量指纹；仅终态；原子整体重写；损坏则备份重来；网络类错误 3 次退避重试。
- [CLI 表面与错误语义](issues/09-cli-surface-and-errors.md)：单命令 + flags；flag > env > 默认；退出码 0/1/2/130；进度 stderr、汇总/`--json` stdout；服务端日志实时透传 stderr（`[service]` 前缀）并落文件。
- [服务启动、就绪与失败形状](issues/07-service-lifecycle.md)：环境已 sync 时 uv 不联网；冷 15.36s / 热 11.92s / preload 28.01s 就绪；就绪前端口拒连（无 503）；端口占用退出码 3、非法 tier 退出码 1；`--tier advanced` 发布四档。
- [Windows 上启停 uv 进程树](issues/08-windows-process-control.md)：`CTRL_BREAK_EVENT` 可优雅停下 uv→python（0.5s 内、无残留）；`taskkill /T /F` 0.28s 强杀；宿主被强杀会留孤儿。
- [残留服务识别与清理](issues/11-residual-service-cleanup.md)：pid 存活 + 端口 health 200 + 命令行含 `mineru-kit api-server --port` 三条同时满足才杀，否则只警告。
- [验收与测试策略](issues/12-acceptance-and-testing.md)：单元 + `httptest` 假服务为默认，真实服务 e2e 走 `-tags=e2e`；验收标准逐条可命令验证。
- [写出 spec.md](issues/10-write-spec.md)：[`spec.md`](spec.md) 已落地——CLI/退出码/发现/状态 schema/服务编排/HTTP 序列/验收标准/已接受风险。

## Not yet specified

（空。当前所有已知问题都已成票或已判定出界。）

## Out of scope

- MinerU 其余 22 种可解析扩展名与图片 OCR：用户明确限定 5 类且不可扩展，属本 effort 之外。
- 远程 mineru.net、外接已有服务实例（`--base-url`）：用户选择总是自启。
- 常驻 watch / 目录索引 / 全文搜索：MinerU 自身 doclib 已有，不在本工具目的内。
- 解压 zip、直出 markdown、把图片内联成 HTML：用户要求 zip 原样落地。
