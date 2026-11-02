# 09 — CLI 表面与错误语义

Type: grilling
Status: resolved
Blocked by: —

## Question

命令行长什么样、配置从哪来、退出码与输出流怎么分？（用户委托我决定）

## Answer

命令：`mineru-ocr <root>... [flags]`（单命令，无子命令）

flags：

- `--workers 2`
- `--tier advanced`（透传给服务端；只影响 PDF）
- `--mineru-project <path>`（默认 `$MINERU_PROJECT`，再默认 `D:/Projects/Python/MinerU`）
- `--port`（默认 0 = 自动挑空闲端口）
- `--dry-run`（只扫描并打印计划，不启动服务）
- `--json`（stdout 输出机器可读报告）
- `--verbose`
- `--service-log <path>`（默认 `<root>/.mineru-service.log`）
- `--no-service-log-stream`（关闭服务日志的实时透传；文件照写）

配置优先级：flag > 环境变量 > 内置默认。无配置文件。无 `--base-url`：服务总是自启。

退出码：`0` 全部成功或无事可做；`1` 至少一个文件失败；`2` 致命（参数非法、服务启动/就绪失败、健康丢失、端口无法分配）；`130` 用户 Ctrl+C。

输出流：**服务端 stdout+stderr 全程实时透传到 stderr**，逐行加 `[service] ` 前缀（启动、模型加载、就绪、运行期、关停都可见），同时原样写入 `--service-log`；进度与错误 → stderr，每文件一行 `[3/42] ok   report.pdf  12.3s` / `[4/42] FAIL scan.docx  code: message`；末尾汇总表 → stdout；`--json` 时 stdout 为单个对象 `{scanned, skipped, completed, failed, files:[{path,status,error,duration_ms,zip_path}]}`。

理由：用户需要靠服务日志判断启动是否卡在模型加载、端口冲突或显存不足；只落文件等于把诊断信息藏起来。

错误分类：

- 服务启动失败 → 透传已在跑，另在 stderr 追加日志尾部（最后 50 行）+ 提示 `uv run --project <path> mineru-kit models download`；退出 2。
- 运行中 `/v1/health` 失败或服务进程退出 → 停止派发，等在飞任务（≤30s），汇总后退出 2。
- 单文件失败 → 记入 state 与报告，继续下一个，最终退出 1。
- Ctrl+C → 取消在飞任务（`POST /v1/tasks/{id}/cancel`）→ 关停服务 → 退出 130。
