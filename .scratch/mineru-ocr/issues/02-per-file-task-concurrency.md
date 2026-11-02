# 02 — 任务粒度与并发

Type: grilling
Status: resolved
Blocked by: —

## Question

一次扫描如何切任务、并发开多少？

## Answer

- **每文件一个 task**，不做批量：失败隔离、单文件重试、进度准确，崩溃后只需重做未完成文件。
- Go 侧固定 worker 池，默认 **2**，`--workers` 可改。
- 服务端启动时 `--concurrency` 与 `--workers` 同值（默认 2），否则多出的 worker 只是排队。
- 每个 worker 独立持有 HTTP 客户端状态与重试计数，互不影响；任务级取消只作用于自己的 task_id。
