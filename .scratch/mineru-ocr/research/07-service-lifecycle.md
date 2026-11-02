# 07 — 服务启动、就绪与失败形状（实测）

环境：Windows 10，Go 1.27.3，uv 0.13.6，CUDA 可用；MinerU 源码 `D:/Projects/Python/MinerU`，命令统一为 `uv run --project D:/Projects/Python/MinerU mineru-kit api-server ...`。所有实验端口用 1808x，结束后 `taskkill /T /F /PID` 清理并确认端口释放。

## 1. 冷/热启动耗时（不带 `--preload-models`）

命令：
```sh
uv run --project D:/Projects/Python/MinerU mineru-kit api-server --port 18081 --tier advanced
```
就绪判定：`curl http://127.0.0.1:18081/v1/health` 首次返回 `200`。

| 场景 | 到 200 耗时 | 备注 |
|---|---|---|
| 冷启动（进程刚起） | 15.36 s | 从调用到 200 |
| 热启动（同一命令第二次） | 11.92 s | 从调用到 200 |

原始输出（冷）：
```text
INFO:     Started server process [21288]
INFO:     Waiting for application startup.
INFO:     Application startup complete.
INFO:     Uvicorn running on http://127.0.0.1:18081 (Press CTRL+C to quit)
INFO:     127.0.0.1:55689 - "GET /v1/health HTTP/1.1" 200 OK
```

uv sync/build：两次日志中均**没有** `Building` / `Installed` / `Resolved` 行，说明在本机已 sync 的前提下 `uv run --project` 不会重新构建或访问网络。结论 6 见下。

## 2. 带 `--preload-models` 的启动

命令：
```sh
uv run --project D:/Projects/Python/MinerU mineru-kit api-server --port 18082 --tier advanced --preload-models
```

- 到 `/v1/health` 返回 `200` 耗时：**28.01 s**（实测 24.84 s 时仍 `000`，28.01 s 时 `200`）。
- 就绪前 `/v1/health`：**连接被拒绝（curl `000`），不是 503**。在 uvicorn lifespan 完成前服务端口不接受连接；因此无法观测到 503。启动完成后直接 `200`。
- 健康响应体（就绪后）：
```json
{"status":"ok","version":"4.1.3","features":{"webhook":false,"task_output_formats":["default","docx","html","latex"],"sources":["upload","url"]}}
```

就绪前日志关键行（原样）：
```text
INFO:     Waiting for application startup.
INFO:     Initializing VLM client and local models for startup tier standard
2026-11-02 20:22:00.145 | INFO | mineru.model.vlm.selector:get_vlm_engine:61 - Using lmdeploy-engine as the inference engine for VLM.
2026-11-02 20:22:00.154 | INFO | mineru.model.vlm.runtime:_create_model:382 - lmdeploy device is: cuda, lmdeploy backend is: turbomind
INFO:     Model initialization completed for startup tier standard (engine=lmdeploy-engine)
INFO:     Application startup complete.
INFO:     Uvicorn running on http://127.0.0.1:18082 (Press CTRL+C to quit)
```
preload 确实加载了 VLM（lmdeploy/cuda）与本地 Hybrid/ONNX 模型（日志含 `Table ONNX ready`），额外耗时约 12.6 s（28.01 − 15.36）。显存未单独测量，源码未输出显存统计。

503 的来源（源码推断，`mineru/parser/api_server.py:1353-1362`）：`/v1/health` 调用 `_require_model_preload_ready`，仅当 `model_preload_error` 非空或 `execution_store.engine_error` 非空时抛 `503`；正常启动路径不会返回 503。preload 失败时错误码由 `_classify_model_preload_error`（`mineru/parser/api_server.py:1712-1724`）给出：`model_preload_dependency_missing` / `model_preload_files_missing` / `model_preload_device_unavailable` / `model_preload_failed` / `engine_dead`。

## 3. `/v1/tiers` 实际响应

命令（服务以 `--tier advanced` 启动）：
```sh
curl http://127.0.0.1:18084/v1/tiers
```
原始响应：
```json
{"object":"list","data":[
  {"id":"flash","description":"Fast local text extraction.","current_model":"mineru-4.1.3"},
  {"id":"basic","description":"Basic parsing with local lightweight models.","current_model":"mineru-4.1.3"},
  {"id":"standard","description":"Standard parsing for most documents.","current_model":"mineru-4.1.3"},
  {"id":"advanced","description":"Advanced parsing for difficult documents.","current_model":"mineru-4.1.3"}
]}
```
四档，顺序 `flash → basic → standard → advanced`，与 `TIERS`（`mineru/types.py:94-99`）及 `TIERS_BY_SERVER_TIER["advanced"]`（`mineru/types.py:130`）一致。

## 4. 可用于就绪判定的信号

最可靠：HTTP `GET /v1/health` 返回 `200`（响应体含 `status: ok`）。备选日志信号（任一出现即可认为启动完成）：
1. `INFO:     Application startup complete.`
2. `INFO:     Uvicorn running on http://127.0.0.1:<port> (Press CTRL+C to quit)`
3. preload 模式下再加：`INFO:     Model initialization completed for startup tier standard (engine=lmdeploy-engine)`

端口可连但 HTTP 未就绪的情况未观测到；正常路径下端口在就绪前完全拒绝连接。

## 5. 失败形状

### 端口被占用
先用 `python -m http.server 18087 --bind 127.0.0.1` 占用端口，再启动 MinerU 同端口：
- 退出码：**3**
- 关键 stderr：
```text
INFO:     Application startup complete.
ERROR:    [Errno 10048] error while attempting to bind on address ('127.0.0.1', 18087): [winerror 10048] 通常每个套接字地址(协议/网络地址/端口)只允许使用一次。
INFO:     Waiting for application shutdown.
INFO:     Application shutdown complete.
```
（Windows 下 WinError 10048；Linux/macOS 对应 `Address already in use`。）

### 非法 `--tier`
命令：`... --tier bogus`
- 退出码：**1**
- stderr：
```text
错误: Unsupported tier 'bogus'. Supported tiers: flash, basic, standard, advanced
```
源码：`validate_tier`（`mineru/types.py:155-160`）抛 `ValueError`，CLI 层转为 `click.ClickException`（`mineru/parser/api_server.py:2221-2224`），退出码 1。

### 模型缺失 / GPU 不可用（源码推断，未实测）
- 预检阶段（`_preflight_tier_dependencies`，`mineru/parser/api_server.py:1676-1686`）：`TierDependencyError` 转为 `ParseServerStartupError` → `click.ClickException` → **退出码 1**，stderr `Error: <exc message>`。
- preload 阶段（`_classify_model_preload_error`，`mineru/parser/api_server.py:1712-1724`）：
  - `ModuleNotFoundError`/`ImportError` → 码 `model_preload_dependency_missing`
  - `FileNotFoundError` → 码 `model_preload_files_missing`
  - 消息含 cuda/device/gpu 等 → 码 `model_preload_device_unavailable`
  - 致命引擎错误（`is_fatal_engine_error`） → 码 `engine_dead`
  - 其他 → `model_preload_failed`
  - preload 失败时服务进程**不一定退出**，`/v1/health` 返回 `503`，body 为 `{"error":{"code":<上列>,"message":...}}`（源码推断，`mineru/parser/api_server.py:1353-1362`）。
- 另：同一机器上第二个 MinerU 实例会因上传目录文件锁失败（`RuntimeError: Upload directory is already owned by another API service`，`mineru/parser/api_server.py:373-375`），退出码 3，stderr 含 `Application startup failed. Exiting.`。

## 6. `uv run --project` 是否访问网络

本机已 sync 的情况下，冷/热/preload 三次启动日志均未出现 `Building`/`Installed`/`Resolved`/网络相关行，stderr 只有 Python/uvicorn 日志。结论：**在环境已就绪时，启动路径不访问网络**；若 `.venv` 或 lock 缺失，uv 可能先 sync（会访问网络/缓存），日志会出现 `Building`/`Installed` 行，应据此判定。

## 编排建议

- 启动：`uv run --project D:/Projects/Python/MinerU mineru-kit api-server --port <free> --tier advanced`（加 `--preload-models` 可把 VLM/本地模型预加载，启动 +12.6 s，但首次请求更快）。
- 就绪判定：轮询 `GET /v1/health` 直到 `200`，超时建议 ≥ 60 s（preload 模式 ≥ 45 s）；日志 `Application startup complete.` 可作辅助。
- 失败处理：退出码 3 + WinError 10048 → 端口占用；退出码 1 + `Unsupported tier` → 参数非法；503 + `model_preload_*` → 模型/设备问题。
- 清理：`taskkill /T /F /PID <pid>`，确认端口返回 `000` 后再启动下一个实例（文件锁也要求串行）。