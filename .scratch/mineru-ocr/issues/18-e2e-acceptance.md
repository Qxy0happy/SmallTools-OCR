# 18 — 真实服务 E2E 与验收

Type: task
Status: resolved
Blocked by: 17

规格：[`../spec.md`](../spec.md) §10。

## 要做什么

1. `internal/testfixture`（或 `testdata`）生成器：最小 1 页 PDF、最小 docx（zip 容器），供 e2e 使用。
2. `e2e_test.go`（`//go:build e2e`）：调用 `run.Run` 以真实 `service.New` 启动 `uv run --project <MINERU_PROJECT> ...`，对一个临时目录跑通：zip 落地 → 二次运行全跳过 → 破坏 zip 后重做；断言 §10 的退出码与产物。
3. 手工验收脚本不需要；但 §10 每条在测试名或注释里可追溯。

## 验收

- `go test -tags=e2e ./...` 在 MinerU 环境可用时通过（本机 `MINERU_PROJECT=D:/Projects/Python/MinerU`）；默认 `go test ./...` 不触发 e2e。
- 跑完后无 `uv`/`python` 残留、端口释放。
- `go build ./... && go vet ./... && gofmt -l . && go test ./...` 全绿。

## Answer

已实现 `internal/testfixture`（最小 1 页 PDF 与 docx 生成器）与 `internal/e2e/e2e_test.go`（`//go:build e2e`）：真实 `uv run --project` 启动服务 → 两个文件解析成功 → 二次全跳过 → 破坏 zip 后只重做该文件 → 端口释放、无 uv/python 残留、stderr 含 `[service]` 行。

实跑证据：`go test -tags=e2e ./internal/e2e -count=1` → `ok ... 124.130s`；日志可见 `Application startup complete.`、`POST /v1/tasks` 202、`PUT .../uploads/...` 200、`GET /v1/files/.../content` 200、`[1/1] ok a.pdf 9.2s`。
