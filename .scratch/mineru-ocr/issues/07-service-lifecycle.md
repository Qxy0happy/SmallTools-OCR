# 07 — 服务启动、就绪与失败形状

Type: research
Status: resolved
Blocked by: —

## Question

程序要自己启动 `uv run --project D:/Projects/Python/MinerU mineru-kit api-server`，需要以下实测事实才能定稿编排逻辑：

1. `uv run --project` 每次调用是否会重新 sync/构建？冷启动与热启动到就绪各耗时多少（分别测 `--preload-models` 与不带）？
2. 就绪判定用哪条信号最可靠：`/v1/health` 返回 200、某个日志行、端口可连？未就绪时 `/v1/health` 是否稳定返回 503？
3. `--preload-models` 是否真在启动阶段加载 VLM 与本地模型？多花多少时间、占多少显存？
4. `--tier advanced` 是否被接受，且 `/v1/tiers` 是否发布四档？
5. 端口被占、模型缺失、GPU 不可用时，进程退出码与 stderr 形状是什么？（程序要据此给用户可操作提示）

## Answer

实测（`.scratch/mineru-ocr/research/07-service-lifecycle.md`，Windows 10 / uv 0.13.6 / CUDA）：

1. **`uv run --project` 在环境已 sync 时不重建、不联网**：三次启动日志均无 `Building` / `Installed` / `Resolved`。只有 `.venv`/lock 缺失时才会 sync（日志出现这些行，可作为诊断信号）。
2. **启动耗时**（到 `GET /v1/health` 返回 200）：冷 15.36s，热 11.92s；带 `--preload-models` 28.01s（VLM lmdeploy/cuda + 本地 ONNX 模型，多 12.6s）。
3. **就绪前端口直接拒绝连接（curl `000`），不会返回 503**；uvicorn lifespan 完成后一次性变 200。因此就绪探针 = 重试连接 + 200，不要等 503。
4. **`/v1/tiers` 在 `--tier advanced` 下发布四档**，顺序 `flash → basic → standard → advanced`。
5. **失败形状**：端口占用 → 退出码 **3** + `WinError 10048`；非法 tier → 退出码 **1** + `Unsupported tier`；模型/设备预检失败 → 退出码 **1**（源码推断）。

编排结论（写入 spec）：

- 启动命令：`uv run --project <path> mineru-kit api-server --port <free> --tier advanced --concurrency <workers> --preload-models`。
- 就绪：轮询 `GET /v1/health`（连接失败也算未就绪），**超时 120s**；超时或进程先退出 → 读日志尾部 → 退出 2。
- 退出码 3 → 换端口重试一次；退出码 1 且 stderr 含 `Unsupported tier` → 不重试，直接致命。
- 关停后必须确认端口不再连通，再做下一实例（ticket 08 的序列）。
