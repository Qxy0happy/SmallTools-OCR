# 05 — 文件发现范围

Type: grilling
Status: resolved
Blocked by: —

## Question

递归扫描认哪些文件，跳过什么？

## Answer

- 扩展名固定：`.pdf`、`.doc`、`.docx`、`.ppt`、`.pptx`，大小写不敏感，**不可通过 flag 扩展**。
- 参数是一个或多个根路径；根可以是文件（只处理该文件）。
- 跳过：目录名以 `.` 开头（含 `.git`）、符号链接（链接目录与链接文件都不跟随）、不可读目录（stderr 警告后继续）。
- 不特殊处理：`.mineru-state.json`、`.mineru-service.log`、`*.part` 天然不匹配扩展名，无需特判。
- 遍历结果按绝对路径排序后送入 worker 池，保证同目录两次运行顺序稳定。
- 每个根读取自己的 `<root>/.mineru-state.json`；同一次运行内跨根按绝对路径去重。
