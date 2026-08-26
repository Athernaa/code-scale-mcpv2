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
go run ./cmd/code-scale-bench run --task fivem_callback_flow --budgets 512,1024,2048,4000,8000,16000,32000 --repeat 2
go run ./cmd/code-scale-bench run --task fivem_focused_inventory --budgets 512,1024,2048,4000,8000,16000,32000
```

The harness builds temporary SQLite indexes from the committed fixtures and removes them after the run. It does not modify indexed fixture source, call an LLM, access the network, invoke jCodeMunch, or require Repomix.

Modes:

- `manual`: deterministic minimum context from manually reviewed required/relevant files; this is not a simulated model.
- `panoramic`: all source files in the task's scoped fixture repository.
- `scoped_panoramic`: the fair relevant-file snapshot for tasks whose scope is known.
- `primitive`: existing storage symbol/semantic search, bounded source reads, and bounded relationship traces.
- `phase7`: the production Planner, ContextAssembler, TokenCounter, and Sufficiency path.
- `phase7_no_early_stop`: benchmark-only paired mode using the same Planner/ranking policy while continuing eligible packing stages; it is not a production MCP mode.

The corpus contains 34 tasks across small generic/FiveM fixtures, a realistic FiveM fixture of 56 indexed files and roughly 2,800 coherent lines, and repository-isolation/adversarial cases. Every task is classified as `supported_deterministic`, `diagnostic_open_ended`, or `adversarial_safety`; every non-supported task requires an exclusion reason. `adversarial_safety` tasks are excluded from supported-recall acceptance but still participate in hard safety gates.

The official matrix is 34 tasks × 6 modes × 7 budgets (`512,1024,2048,4000,8000,16000,32000`) × 2 repeats. `--output` writes JSON and `--markdown` writes the human-readable report. The default report paths are `benchmarks/reports/latest.json` and `benchmarks/reports/latest.md`.

Reported metrics include required dependency/symbol/file/relationship/provider recall, precision with undefined empty retrievals excluded, Top-5/Top-10 recall, serialized/source/metadata tokens, broad and scoped panoramic savings, small/realistic/combined strata, sufficiency status and stop stage, false sufficiency/insufficiency, source reads/bytes, logical primitive calls, latency, provider fabrication, repository leakage, determinism, budget monotonicity, early-stop tokens/reads/rounds avoided, panoramic/scoped self-baseline accounting, and incremental-vs-full FiveM authority/context equivalence.

Ground truth is independent of Planner output. Relationship coverage requires actual indexed edge evidence and endpoint source inclusion; endpoint co-occurrence and source-text mentions do not count. Reports record the full tested commit SHA, tree SHA, corpus/fixture revision, tokenizer, budgets, repeats, and dirty-worktree state.
