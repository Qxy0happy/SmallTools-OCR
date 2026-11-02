# 08 — Windows 上启停 uv 进程树

Type: research
Status: resolved
Blocked by: —

## Question

Go 在 Windows 启动 `uv run --project ... mineru-kit api-server` 后，如何可靠停止整棵进程树？

1. `CREATE_NEW_PROCESS_GROUP` + `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT)` 是否能让 uv 把它转发给 python 子进程，uvicorn 优雅退出（端口释放、无残留、显存释放）？
2. 若 uv 不转发，`taskkill /T /F` 之后的残留情况如何（是否有孤儿 python 占端口/显存）？
3. 优雅退出需要等多久，超时阈值该设多少？
4. 宿主 Go 进程被强杀（任务管理器结束）时，服务进程会不会随之消失？若不会，下次启动如何识别并清理残留？

## Answer

实测（本机 Windows 10 / Go 1.27.3 / uv 0.13.6，完整证据见 `.scratch/mineru-ocr/research/08-windows-process-control.md`）：

1. **优雅关停可行**：`CREATE_NEW_PROCESS_GROUP` + `GenerateConsoleCtrlEvent(CTRL_BREAK_EVENT, uv_pid)` 后，uv 退出，python 子进程收到信号 21（`SIGBREAK`）并优雅退出，无 `python.exe`/`uv.exe` 残留。
2. **强杀可靠但无优雅**：`taskkill /T /F /PID <uv pid>` 能终止整棵进程树，约 0.28s，端口立即释放，但子进程没有信号处理机会（无 shutdown 日志）。
3. **阈值**：真实 api-server 健康 200 约 2.03s；`CTRL_BREAK` 后端口释放约 0.50s。选 **5s** 优雅等待上限，超时才 `taskkill /T /F`。
4. **宿主被强杀会留孤儿**：对 Go 宿主 `taskkill /F` 后，uv 与 python **全部存活**。因此下次启动必须主动识别清理（见 ticket 11）。

结论（写入 spec 的关停序列）：`CTRL_BREAK_EVENT` → 等 5s → `taskkill /T /F`；启动时按 ticket 11 的三条判据清理残留，绝不按 pid 盲杀。
