# 11 — 残留服务识别与清理

Type: grilling
Status: resolved
Blocked by: 08-windows-process-control

## Question

宿主被强杀后 `uv` 与 `python` 都会存活（ticket 08 实测），下次运行如何安全地识别并清理自己的残留，而不误杀别人的服务？

## Answer

- 启动时立刻把 `service{pid, port, started_at}` 写入每个扫描根的 state；多根时写入相同内容，清理时按 pid 去重。
- 下次启动的判定，必须**三条同时满足**才认定为自家残留：
  1. 记录的 pid 仍存活；
  2. 记录端口上 `GET /v1/health` 返回 200；
  3. 该 pid 的 `Win32_Process.CommandLine` 同时含 `mineru-kit`、`api-server` 与 `--port <记录端口>`。
- 命中则按 ticket 08 的序列关停（`CTRL_BREAK_EVENT` → 等 5s → `taskkill /T /F`）。
- 任一条件不满足：不杀，stderr 警告并继续；**绝不按 pid 盲杀**。
- 随后照常挑新空闲端口、启动新实例、覆写 `service` 记录。
- 不做全系统 python 进程枚举。

证据：`.scratch/mineru-ocr/research/08-windows-process-control.md`（实验 4：宿主强杀后 uv/python 存活；实验 1/2：两种关停路径的耗时与残留）。
