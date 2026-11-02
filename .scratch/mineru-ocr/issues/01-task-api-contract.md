# 01 — 对接的服务与协议

Type: grilling
Status: resolved
Blocked by: —

## Question

Go 程序把文件交给本地 MinerU 的哪个接口？需要哪些端点、状态语义与限制？

## Answer

选 `uv run mineru-kit api-server`（默认 127.0.0.1:8000）暴露的 **Tasks 协议**；不用 `file-parse-server`（它是 Tasks 的同步包装，需要额外上游、多一跳），也不用远程 mineru.net。

每文件一条任务的调用序列：

1. `POST /v1/tasks`，`Content-Type: application/json`
   ```json
   {"files":[{"file_name":"report.pdf","bytes":123456}],"tier":"advanced","ocr_mode":"auto","page_range":"","output_formats":["default"]}
   ```
   → `202`：`{task_id, status, files:[{file_id, file_name, status:"waiting-file", upload:{url, headers:{"Content-Type":"application/octet-stream"}, expires_at}}]}`
2. `PUT <upload.url>`，`application/octet-stream`，`Content-Length` 必须等于声明的 `bytes` → `{task_id, file_id, status}`
3. `GET /v1/tasks/{task_id}` 轮询：`status ∈ {waiting-file,pending,running,completed,partial,failed,canceled}`，`progress:{completed,failed,total}`，`files[0].{status,parse:{tier,duration_ms,parser_version},result_url,error:{code,message,param}}`
4. `GET <result_url>`（即 `/v1/files/{id}/content`）下载 zip。

限制与语义：

- 每任务 ≤100 文件（`MAX_TASK_FILES`）；本设计每任务 1 文件。
- 上传链接 TTL 3600s（`TASK_UPLOAD_TTL`）；终态任务保留 86400s（`--retention-seconds`）。
- 单文件任务不会出现 `partial`；该状态只在多文件混合结果时产生。
- 取消：`POST /v1/tasks/{task_id}/cancel`。
- 错误体统一 `{"error":{"code","message","param"}}`；可重试状态 408/429/500/502/503/504，参考客户端 `mineru/parser/api_client.py` 的退避参数（1s 轮询、0.25s/0.5s/1s 传输重试）。
- 服务端并发由启动参数 `--concurrency` 决定（默认 1）。

来源：`mineru/tasks/http.py`、`mineru/tasks/contracts.py`、`mineru/tasks/service.py`、`mineru/parser/task_backend.py`、`mineru/parser/api_client.py`、`mineru/filetypes.py`。
