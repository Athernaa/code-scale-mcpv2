# Phase 7.4 deterministic benchmark

This directory contains the offline Phase 7.4 corpus, fixtures, and generated validation reports. The corpus ground truth is manually authored in `corpus.json`; it is never derived from Planner output.

Run the full default matrix:

```text
go run ./cmd/code-scale-bench run
```

Useful scoped runs:

```text
go run ./cmd/code-scale-bench run --category fivem_export_flow
go run ./cmd/code-scale-bench run --mode phase7 --repeat 5
go run ./cmd/code-scale-bench run --task fivem_focused_inventory --budgets 512,1024,2048,4000,8000,16000,32000
```

The harness builds temporary SQLite indexes from the committed fixtures and removes them after the run. It does not modify indexed fixture source, call an LLM, access the network, invoke jCodeMunch, or require Repomix.

Modes:

- `manual`: deterministic minimum context from manually reviewed required/relevant files; this is not a simulated model.
- `panoramic`: all source files in the task's scoped fixture repository.
- `primitive`: existing storage symbol/semantic search, bounded source reads, and bounded relationship traces.
- `phase7`: the production Planner, ContextAssembler, TokenCounter, and Sufficiency path.

The default matrix is 30 tasks × 4 modes × 4 budgets. `--output` writes JSON and `--markdown` writes the human-readable report. The default paths are `benchmarks/reports/latest.json` and `benchmarks/reports/latest.md`; those files are generated validation artifacts, not runtime inputs.

Reported metrics include required dependency/symbol/file/relationship/provider recall, precision, Top-5/Top-10 recall, serialized/source/metadata tokens, broad-baseline savings, sufficiency status and stop stage, false sufficiency/insufficiency, source reads/bytes, logical primitive calls, latency, provider fabrication, repository leakage, determinism, budget monotonicity, and incremental-vs-full FiveM authority/context equivalence.
