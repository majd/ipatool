# AGENTS.md

This file provides guidance for coding agents working in this repository.

## Repository overview
- Project: `ipatool`
- Language: Go
- Entry point: `main.go`
- CLI command implementations: `cmd/`

## Development workflow
1. Keep changes focused and minimal.
2. Prefer idiomatic Go and keep command behavior consistent with existing commands in `cmd/`.
3. Run formatting and tests before finalizing changes.

## Local checks
Use these commands from the repository root:

```bash
go generate ./...
go test ./...
go build ./...
```

## Coding conventions
- Follow standard Go formatting (`gofmt`).
- Avoid introducing new dependencies unless necessary.
- Keep user-facing text consistent with existing CLI help/output tone.
- Preserve backward compatibility for CLI flags and output formats unless explicitly asked to change them.

## Commit/PR guidance
- Write clear, scoped commit messages.
- Summarize what changed and why in PR descriptions.
- Include test/build results in your handoff.
