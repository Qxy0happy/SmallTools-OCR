# 10 — 写出 spec.md

Type: task
Status: resolved
Blocked by: 07-service-lifecycle, 08-windows-process-control

## Question

把已锁定的决策写成可直接实现的 `.scratch/mineru-ocr/spec.md`：

- CLI 契约（flags、默认值、配置优先级、退出码、输出流）
- HTTP 交互序列（create → upload → poll → download）与错误码映射
- state schema 与增量/重试规则
- 服务编排（启动命令、就绪探针、心跳、关停、残留清理）
- 验收标准：每条都能用命令或读结果验证

## Answer

规格已写入 [`.scratch/mineru-ocr/spec.md`](../spec.md)，包含：CLI 与配置优先级、退出码与输出流、发现规则、xxh3 状态 schema 与跳过判据、服务编排（残留清理 → 启动 → 就绪 → 存活 → 关停）、每文件 HTTP 序列与重试分类、并发模型、实现约束、10 条可命令验证的验收标准、3 条已接受风险。

所有前置决策（01–09、11、12）均已 resolved；map 前沿为空。

补充（用户追加要求）：服务端日志必须**实时输出**，不能只落文件。规格 §3/§6.2 已改为 stdout+stderr 双写（`[service] ` 前缀透传 stderr），并新增 `--no-service-log-stream` 与对应验收标准；ticket 09 的答案同步更新。
