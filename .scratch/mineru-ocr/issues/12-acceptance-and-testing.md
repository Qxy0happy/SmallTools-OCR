# 12 — 验收与测试策略

Type: grilling
Status: resolved
Blocked by: 07-service-lifecycle

## Question

一个要自起 GPU 服务、每轮约 28s 启动的 Go 工具，测试金字塔怎么摆？规格里的验收标准写成什么才可执行？

## Answer

三层，默认只跑前两层：

1. **单元**（无网络、无进程）：扩展名匹配与排序、zip 命名、state 读写/损坏回退、xxh3 指纹、跳过判据、错误分类（可重试 vs 不可重试）。
2. **集成**（`httptest` 假 Tasks 服务）：断言 create 请求体（PDF 带 `tier=advanced`、Office 不带）、上传 `Content-Length`、轮询状态机、`result_url` 下载与 zip 校验、`partial`/失败映射、退避重试次数、退出码与 `--json` 报告形状。
3. **E2E**（`-tags=e2e`，手动或 CI 可选）：真实 `uv run --project ... mineru-kit api-server` + fixture（一个 1 页 PDF + 一个 docx）；断言 zip 落地、二次运行全跳过、故意破坏 zip 后重做。

验收标准（写进 spec，逐条可命令验证）：

- `go test ./...` 全绿；集成测试不依赖 GPU。
- `go run . <fixture> --dry-run` 只打印计划、不启动服务（无 uv 进程）。
- 首次 `go run . <fixture>` 生成 `<basename>.<ext>.zip`，退出 0，state 记录 completed。
- 第二次运行全部跳过，耗时 < 2s（不含服务启动可另行断言），退出 0。
- 删除其中一个 zip 后重跑：该文件重做，其余仍跳过。
- 服务端不可启动（`--mineru-project` 指向空目录）→ 退出 2，stderr 含日志尾部与 `mineru-kit models download` 提示。
- Ctrl+C：在飞任务被取消、服务进程退出、无残留进程、退出 130。

依据：ticket 07 的实测启动成本（冷 15.36s / preload 28.01s）决定了默认测试不得依赖真实服务。
