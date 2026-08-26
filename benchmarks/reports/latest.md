# Phase 7.4 Final Validation

CONDITIONAL PASS

## Code-Scale Commit

`34b5228`

## Corpus Version

`7.4.0` / fixture `phase7.4-initial` / schema `1`

## Benchmark Environment

Go `go1.26.7`, tokenizer `o200k_base`, repeats `2`, budgets `[512 1024 2048 4000 8000 16000 32000]`.

## Corpus Summary

Tasks selected: **30**. Results recorded: **1680**.

## Categories

- `broad_architecture`: 14 results, median recall 66.7%, median saving -165.3%, false sufficiency 0.
- `callee_trace`: 14 results, median recall 100.0%, median saving -69.2%, false sufficiency 0.
- `caller_trace`: 14 results, median recall 100.0%, median saving -33.3%, false sufficiency 0.
- `client_server_side_provider`: 14 results, median recall 100.0%, median saving -48.0%, false sufficiency 0.
- `command`: 14 results, median recall 100.0%, median saving 8.2%, false sufficiency 0.
- `custom_framework_operation`: 14 results, median recall 100.0%, median saving 36.8%, false sufficiency 0.
- `duplicate_symbol`: 14 results, median recall 50.0%, median saving -18.4%, false sufficiency 0.
- `dynamic_provider`: 14 results, median recall 100.0%, median saving 1.0%, false sufficiency 0.
- `exact_semantic_endpoint`: 14 results, median recall 100.0%, median saving -29.4%, false sufficiency 0.
- `exact_symbol_lookup`: 14 results, median recall 100.0%, median saving 7.4%, false sufficiency 0.
- `execution_side_provider`: 14 results, median recall 100.0%, median saving 61.9%, false sufficiency 0.
- `external_provider`: 14 results, median recall 100.0%, median saving -48.9%, false sufficiency 0.
- `fivem_callback_flow`: 14 results, median recall 100.0%, median saving 39.0%, false sufficiency 2.
- `fivem_event_flow`: 14 results, median recall 100.0%, median saving -20.0%, false sufficiency 0.
- `fivem_export_flow`: 14 results, median recall 100.0%, median saving 30.8%, false sufficiency 0.
- `focused_multi_resource_feature`: 14 results, median recall 100.0%, median saving -13.0%, false sufficiency 0.
- `framework_facade_chain`: 14 results, median recall 100.0%, median saving 55.9%, false sufficiency 0.
- `generic_refactor`: 14 results, median recall 0.0%, median saving 37.2%, false sufficiency 0.
- `impact_blast_radius`: 14 results, median recall 0.0%, median saving 75.3%, false sufficiency 0.
- `known_framework_operation`: 14 results, median recall 0.0%, median saving 76.1%, false sufficiency 0.
- `lua_generic_feature`: 14 results, median recall 100.0%, median saving -401.2%, false sufficiency 0.
- `medium_feature`: 14 results, median recall 100.0%, median saving -106.9%, false sufficiency 0.
- `multi_resource_feature`: 14 results, median recall 0.0%, median saving 68.6%, false sufficiency 0.
- `narrow_bug_fix`: 14 results, median recall 100.0%, median saving -101.1%, false sufficiency 0.
- `nui_callback`: 14 results, median recall 100.0%, median saving -15.7%, false sufficiency 0.
- `provider_ambiguity`: 14 results, median recall 100.0%, median saving -48.7%, false sufficiency 0.
- `repo_isolation`: 14 results, median recall 100.0%, median saving -837.5%, false sufficiency 0.
- `typescript_api`: 14 results, median recall 100.0%, median saving -49.8%, false sufficiency 0.
- `typescript_feature`: 14 results, median recall 100.0%, median saving -273.1%, false sufficiency 0.
- `verified_cross_resource_call`: 14 results, median recall 100.0%, median saving 31.4%, false sufficiency 0.

## Ground Truth Method

Ground truth is manually authored in `benchmarks/corpus.json`; it is not generated from Planner output. Required items are scored separately from relevant files and unrelated fixture noise.

## Modes Compared

- **manual**: 420 results, median recall 100.0%, median saving 73.8%.
- **panoramic**: 420 results, median recall 100.0%, median saving 0.0%.
- **primitive**: 420 results, median recall 100.0%, median saving 81.1%.
- **phase7**: 420 results, median recall 100.0%, median saving -7.5%.

### Manual/Baseline

Deterministic ground-truth minimum files; no model behavior is simulated.

### Panoramic / Repomix-Like

Offline scoped broad-file snapshot; Repomix is optional and not required.

### Primitive Code-Scale

Existing storage symbol/semantic search, bounded source reads, and bounded relationship traces.

### Phase-7 assemble_context

Production Planner, ContextAssembler, TokenCounter, and Sufficiency path.

## Retrieval Quality

- Required dependency recall across the complete budget matrix: **75.4% mean / 100.0% median**.
- Precision: **100.0% median**.
- Top-5 recall: **100.0% median**.
- Top-10 recall: **100.0% median**.
- Supported-task acceptance recall is evaluated at each task's maximum requested budget; small-budget omissions remain visible in the raw results.

## Token Efficiency

- Median Phase-7 context tokens: **580**.
- Median reduction versus broad baseline across the full matrix: **-7.5%**.
- Supported-task median reduction at maximum budget: **-104.0%**.
- Distribution: p25 **-48.9%**, p75 **37.2%**, p90 **68.2%**.

## Sufficiency

Sufficient **202**, blocked **188**, indeterminate **30**, false sufficiency **2**, false insufficiency **102**.

## Early Stop

Median retrieval rounds **1.0**, median source reads **2.0**, median latency **6.9 ms**, p95 latency **23.7 ms**.

## Provider Correctness

Fabrication cases: **0**.

## Cross-Resource Correctness

Cross-repository leaks: **0**.

## Execution-Side Correctness

Provider and side cases are included in the adversarial corpus; authority is scored from persisted semantic metadata and returned candidates.

## Incremental vs Full Index

Mismatches: **0**.

## Repo Isolation

Leaks: **0**.

## Budget Compliance

Serialized budget violations: **0**.

## Determinism

Failures: **0**.

## Latency

Latency is reported descriptively and is not used as a fragile pass/fail threshold.

## Generic Go Results

See category/task rows in the machine-readable report.

## TypeScript Results

See category/task rows in the machine-readable report.

## Lua Results

See category/task rows in the machine-readable report.

## FiveM Results

See category/task rows in the machine-readable report.

## Framework Results

Provider, authority, and execution-side tasks are scored separately from generic recall.

## Adversarial Results

Duplicate symbols, duplicate providers, external/dynamic providers, wrong-side calls, and repository markers are explicit corpus cases.

## Metamorphic Results

The runner checks repeated-output fingerprints, budget recall monotonicity, and an incremental-refresh versus fresh-full-index FiveM comparison.

## Benchmark-Discovered Bugs

- Harness defect fixed: metamorphic phase7 runs lazily initialize the token counter; the focused rerun passed.
- Harness defect fixed: relationship scoring requires two distinct endpoint occurrences; the focused rerun passed.
- Harness fidelity fixed: incremental/full comparison now includes the real watcher framework refresh path; the metamorphic rerun passed.
- Phase-7 defect fixed: FocusSymbolID now bridges persisted generic, FiveM, and framework facts sharing the source symbol; planner regression and focused FiveM rerun passed.
- Phase-7 defect fixed: final non-debug sufficiency reevaluation could exceed the serialized context budget; final evaluate-then-enforce stabilization and a 1024-token regression now pass.

## Verification Commands

The benchmark CLI is offline and uses the same repository Go toolchain. Release commands are listed in `benchmarks/README.md`.

## jCodeMunch Final Audit

jCodeMunch was used during development for architecture, caller, dependency, changed-symbol, and blast-radius inspection. It is not imported by the harness or runtime.

## Measured Claims

Only the percentages and counts above are claims, and they are computed from this report's task results. No external-agent success or Repomix claim is implied.

## Remaining Limitations

supported-task median token reduction is below the provisional 50% target. External-agent task execution and Repomix comparison were not part of this offline benchmark.

## Final Recommendation

Phase 7.4 produced evidence with one or more provisional acceptance targets unmet; downstream integration requires review of the reported limitations.
