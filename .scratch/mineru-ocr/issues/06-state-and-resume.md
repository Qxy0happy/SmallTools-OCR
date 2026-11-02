# 06 — 状态文件与增量

Type: grilling
Status: resolved
Blocked by: 04-artifact-and-placement

## Question

如何判断「已做过」，中断后如何恢复，失败怎么重试？

## Answer

- 位置：每个扫描根下 `.mineru-state.json`。
- 指纹：**xxh3-128 全量哈希**（`github.com/zeebo/xxh3`）。每次运行对候选文件整体读取，不使用 size/mtime 快路径。
- 每条记录：`path`(绝对)、`size`、`hash`、`tier`、`zip_path`、`status`(`completed`/`failed`)、`error{code,message}`、`duration_ms`、`updated_at`；顶层 `schema_version`、`tool_version`。
- 跳过条件：`status == completed` **且** hash 一致 **且** `zip_path` 存在且能打开。任一不满足则重做并覆盖。
- 只持久化终态：不做 in-flight / task_id 续传；崩溃后未完成文件下次整轮重来。
- 写入：每文件完成后整体原子重写（tmp + rename），不做 append。
- state 文件损坏（JSON 解析失败）：`.mineru-state.json` → `.mineru-state.json.bak`，从空状态开始并 stderr 警告，不中止整轮。
- 重试：仅网络类错误（连接失败、408/429/5xx、上传中断）重试，每文件最多 3 次，退避 0.25s/0.5s/1s；解析类 4xx 与 zip 校验失败不重试。
