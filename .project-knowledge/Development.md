---
type: development-workflow
project: code-scale-mcpv2
status: active
updated: 2026-08-26
tags:
  - project/code-scale-mcpv2
  - project-knowledge
---

# Development

## Current commands
- `make build` builds `bin/code-scale-mcp`.
- `make test` runs `go test ./... -count=1`.
- `make fmt` runs `gofmt -w .`.
- `make lint` runs golangci-lint when installed.
- `make clean` removes build artifacts.
- `go build ./cmd/code-scale-bench` builds the offline benchmark CLI.
- `go run ./cmd/code-scale-bench run --budgets 512,1024,2048,4000,8000,16000,32000 --repeat 2` runs the offline Phase 7.4 matrix and writes `benchmarks/reports/latest.json` plus `benchmarks/reports/latest.md`.
- The committed reference report records the clean implementation SHA separately from the later report commit; benchmark-generated reports must be run before committing the report artifact so `dirty_worktree` remains false.

## Verification strategy
- Prefer existing project commands and test suites.
- Start with the smallest targeted check that can falsify the change.
- Do not persist one-off verification scripts when an inline terminal command is sufficient.
- If a durable regression test is justified, place it in the established test hierarchy and keep scope minimal.

## Local setup
- Record only verified prerequisites and setup steps.

## Conventions
- Preserve repository conventions unless there is a concrete reason to change them.
