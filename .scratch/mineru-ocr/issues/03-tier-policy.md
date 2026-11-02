# 03 — 档位策略

Type: grilling
Status: resolved
Blocked by: —

## Question

PDF 用什么档位、Office 文件怎么处理、档位不可用时怎么办？

## Answer

- 档位固定 **advanced**（Hybrid effort = `xhigh`），由本程序启动服务时以 `--tier advanced` 保证；用户不接受自动降级。
- `advanced` 只对 PDF/图片生效；`doc/docx/ppt/pptx` 在服务端被强制 `flash`（`resolve_task_tier`：全部为轻量格式时直接返回 flash，忽略请求里的 tier）。因此 **PDF 请求体带 `"tier":"advanced"`，Office 文件不带 tier**。
- 服务端未发布 advanced 时，`POST /v1/tasks` 返回 400 `Tier 'advanced' is not available`：记为单文件失败，错误原样进入 stderr 与报告，不重试、不降级。
- 该取舍作为已接受风险记在 map Notes（前提：服务由本程序启动）。
