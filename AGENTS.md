## Quality gates

- `task fmt` — gofumpt 格式化（经 golangci-lint）；`task fmt:check` 只检查不写。
- `task lint` — revive 规则见 `.golangci.yml`。
- `task test` — 单测；`task e2e` — 真实 MinerU 服务 e2e（需 uv、MinerU 项目与本地模型）。
- 交付前跑 `task check`（fmt:check + lint + test），必须 0 issue、全绿。

## Agent skills

### Issue tracker

Issues and specs live as markdown files under `.scratch/<feature>/` in this repo. See `docs/agents/issue-tracker.md`.

### Triage labels

Default five-role vocabulary (`needs-triage`, `needs-info`, `ready-for-agent`, `ready-for-human`, `wontfix`). See `docs/agents/triage-labels.md`.

### Domain docs

Single-context: one `GLOSSARY.md` and `docs/adr/` at the repo root. See `docs/agents/domain.md`.
