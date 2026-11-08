# 19 — Linux 上启停 uv 进程树

Type: task
Status: resolved
Blocked by: —

## Question

把 `mineru-ocr` 的服务进程控制从 Windows 迁到 Linux：优雅关停、强杀、pid 存活判断、残留识别、端口占用判据、默认 MinerU 项目路径，分别怎么落地？

## Answer

用户决策（2026-11-08）：

1. **平台范围**：只留 Linux，删掉 `service_windows.go`；非 Linux 仍以退出码 2 拒绝（`service_other.go` 改 `//go:build !linux`）。
2. **默认项目路径**：`$MINERU_PROJECT` 优先，内置默认 `/home/qxyhappy/Projects/MinerU`。
3. **关停序列**：`SIGTERM` 发给整个进程组 → 等 5s → `SIGKILL` 发给进程组。
4. **孤儿兜底**：不加 `Pdeathsig`（Go 里它在“创建子进程的线程”退出时触发，runtime 线程退役可能提前杀掉服务），只靠启动前三判据清理。
5. **死代码**：顺手删掉 `service.go` 里 15 个无人引用的同义别名变量。
6. **文档**：spec 相应章节改写，本票与 map 记录证据。
7. **验证**：单测 + 真实 e2e（默认 advanced 档）。

本机实测（Linux 7.3.3、uv 0.13.7、RTX 4060 Laptop、Go 1.27，2026-11-08）：

- 进程树：`uv(pid)` → `.venv/bin/python .venv/bin/mineru-kit api-server ...`，`pgid == uv pid`（`Setpgid` 后成立，实测 `setsid` 启动同样成立）；`/proc/<pid>/cmdline` 含 `mineru-kit`、`api-server`、`--port <port>`，所以 §6.1 的三条判据原样可用，只把 `Win32_Process` 换成 `/proc/<pid>/cmdline`。
- 端口冲突：第二个实例 **exit 3**，日志 `ERROR: [Errno 98] ... address already in use`，与 Windows 的 `WinError 10048` 同类；`ClassifyExit` 改成认 `address already in use` 并保留 code 3。
- `kill -TERM <uv pid>`：305ms 内 uv 退出（143），python 打印 `Application shutdown complete.`，端口释放、无残留 —— uv 会转发信号，但我们仍对整组发信号，不依赖它。
- 模型已在本机：`~/.mineru/models` 下 `MinerU2.5-Pro-2605-1.2B`(2.2G) 与 `MinerU-4_models_torch`(854M)，`MINERU_MODEL_SOURCE=modelscope`；advanced 档可离线跑。

代码改动：

- 删 `internal/service/service_windows.go`；`service_other.go` 改为 `!linux` 且 `platformSupported = false`。
- 新增 `internal/service/service_linux.go`：`SysProcAttr{Setpgid:true}`、`gracefulStop`（组 `SIGTERM`）、`defaultPidAlive`（`/proc/<pid>/stat` state 非 `Z`/`X`）、`defaultCommandLine`（`/proc/<pid>/cmdline`）、`defaultKill`（确认 `getpgid(pid)==pid` 时 `kill(-pid, SIGKILL)`，否则逐个杀 pid 与子孙，绝不按组盲杀）。
- `Stop()` 改等 `s.doneCh`：原来的 `os.FindProcess(pid).Wait()` 在 POSIX 上对非子进程立即返回 `wait: no child processes`，会让 5s 宽限形同虚设。
- `ClassifyExit` 增加 `address already in use` 判据。
- `app.Defaults()` 默认项目路径改 `/home/qxyhappy/Projects/MinerU`。
- `internal/e2e/e2e_test.go` 的残留检查从 powershell/Win32_Process 改为扫 `/proc/*/cmdline`。
- 删 `service.go` 的别名变量（`BuildCommand`/`buildCommand`/`buildCommandArgs`/`commandArgs`/`startCommand`/`pollReady`/`WaitReadyPoll`/`classifyExit`/`shouldRetry`/`ReadLogTailLines`/`readLogTailString`/`staleMatchFn`/`cleanupCandidate`/`isStale`/`pickPort`/`getFreePort`）与 `PidAlive`/`Health`/`CommandLine`/`KillProcess` 注入层（`CleanupStale` 的 deps 参数已覆盖注入需求）。

顺带修复的移植问题：`internal/discover/discover_test.go` 断言 3 个文件，依赖“chmod 0000 的目录仍可读”（root/Windows 语义），在普通用户 Linux 上实际只有 2 个；改为断言 2 个并校验不可读目录会产生 1 条警告。
