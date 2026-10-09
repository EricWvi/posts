# AGENTS.md

修改转换、清洗、存储等行为前先读 `docs/design.md`，改了规则要同步更新它。

## Commands

Project commands live in `Taskfile.yml`; run `task --list` to see them all.
Prefer the smallest relevant task while iterating, and run `task test` before
considering a change complete.

- Format Go sources: `task format`
- Frontend typecheck: `task lint:frontend`
- Go gofmt check and vet: `task lint:go`
- Go tests: `task test:go`
- All lint and unit tests: `task test`
- Release binary: `task build`
