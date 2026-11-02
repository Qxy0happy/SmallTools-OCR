# 04 — 产物形态与落点

Type: grilling
Status: resolved
Blocked by: 01-task-api-contract

## Question

拿回来的产物存成什么、放在哪、如何避免半包？

## Answer

- 请求 `output_formats: ["default"]`，服务端返回 `result.zip`，内含 `markdown.md`、`middle_json.json`、`structured_content.json`、`images/**`。
- **不解压**：zip 原样保存。
- 落点：源文件同目录，文件名 `<basename>.<ext>.zip`（`report.pdf` → `report.pdf.zip`，`report.docx` → `report.docx.zip`）；同目录同名不同扩展名的源文件不会互相覆盖。
- 原子性：先写 `<目标>.part` → `zip.OpenReader` 打开成功且至少一个条目 → `rename` 覆盖目标。任何一步失败都不留半包，目标记为失败。
- 已存在目标文件按 ticket 06 的跳过/重做规则处理（hash 一致且 zip 可打开才跳过）。
