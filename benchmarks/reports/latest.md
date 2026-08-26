# Phase 7.4 Final Validation

CONDITIONAL PASS

## Code-Scale Commit

`dc229dee2c56081e8f49d0f03ab5c3468c495e63`

Tree: `e8c212f33f3dcd411708077627f6e678a9e159c8`; dirty worktree: **false**.

## Corpus Version

`7.4.0` / fixture `phase7.4-trustworthy-realistic` / schema `1`

## Benchmark Environment

Go `go1.26.7`, tokenizer `o200k_base`, repeats `2`, budgets `[512 1024 2048 4000 8000 16000 32000]`.

## Corpus Summary

Tasks selected: **34**. Results recorded: **2856**. Supported tasks: **26 total / 26 passed / 0 failed**. Excluded tasks: **8**.

- excluded `broad_architecture_go`: Broad architecture has no complete deterministic dependency boundary.
- excluded `fivem_natural_inventory`: Natural-language no-focus workspace task is intentionally reported as diagnostic/open-ended.

- `realistic` fixtures: 56 results, median recall 100.0%, broad reduction 93.6%, scoped reduction 1.8%.
- `small` fixtures: 420 results, median recall 100.0%, broad reduction -13.7%, scoped reduction -290.4%.

## Categories

- `broad_architecture`: 14 results, median recall 66.7%, broad saving -165.3%, scoped saving -249.7%, false sufficiency 0.
- `callee_trace`: 14 results, median recall 100.0%, broad saving -69.2%, scoped saving -224.2%, false sufficiency 0.
- `caller_trace`: 14 results, median recall 100.0%, broad saving -33.3%, scoped saving -235.3%, false sufficiency 0.
- `client_server_side_provider`: 14 results, median recall 100.0%, broad saving -48.0%, scoped saving -604.5%, false sufficiency 0.
- `command`: 14 results, median recall 100.0%, broad saving 13.5%, scoped saving -702.4%, false sufficiency 0.
- `custom_framework_operation`: 14 results, median recall 100.0%, broad saving 25.4%, scoped saving -805.3%, false sufficiency 0.
- `duplicate_symbol`: 14 results, median recall 50.0%, broad saving -18.4%, scoped saving -551.9%, false sufficiency 0.
- `dynamic_provider`: 14 results, median recall 100.0%, broad saving 1.0%, scoped saving -205.1%, false sufficiency 0.
- `exact_semantic_endpoint`: 14 results, median recall 100.0%, broad saving -29.4%, scoped saving -398.2%, false sufficiency 0.
- `exact_symbol_lookup`: 28 results, median recall 100.0%, broad saving 50.8%, scoped saving -169.0%, false sufficiency 0.
- `execution_side_provider`: 14 results, median recall 100.0%, broad saving 63.5%, scoped saving -942.5%, false sufficiency 0.
- `external_provider`: 14 results, median recall 100.0%, broad saving -48.9%, scoped saving -680.0%, false sufficiency 0.
- `fivem_callback_flow`: 14 results, median recall 100.0%, broad saving 41.1%, scoped saving -309.8%, false sufficiency 0.
- `fivem_event_flow`: 14 results, median recall 100.0%, broad saving -16.1%, scoped saving -652.8%, false sufficiency 0.
- `fivem_export_flow`: 14 results, median recall 100.0%, broad saving 34.6%, scoped saving -242.2%, false sufficiency 0.
- `fivem_multi_resource_feature`: 14 results, median recall 100.0%, broad saving 88.7%, scoped saving 1.8%, false sufficiency 0.
- `focused_multi_resource_feature`: 14 results, median recall 100.0%, broad saving -6.4%, scoped saving -290.4%, false sufficiency 0.
- `framework_facade_chain`: 14 results, median recall 100.0%, broad saving 28.4%, scoped saving -211.8%, false sufficiency 0.
- `generic_refactor`: 14 results, median recall 100.0%, broad saving -107.6%, scoped saving -216.8%, false sufficiency 0.
- `impact_blast_radius`: 14 results, median recall 100.0%, broad saving -27.9%, scoped saving -748.3%, false sufficiency 0.
- `known_framework_operation`: 14 results, median recall 100.0%, broad saving 34.4%, scoped saving -243.6%, false sufficiency 0.
- `lua_generic_feature`: 14 results, median recall 100.0%, broad saving -406.5%, scoped saving -558.5%, false sufficiency 0.
- `medium_feature`: 14 results, median recall 100.0%, broad saving -106.9%, scoped saving -296.5%, false sufficiency 0.
- `multi_resource_feature`: 14 results, median recall 0.0%, broad saving 69.9%, scoped saving 12.3%, false sufficiency 0.
- `narrow_bug_fix`: 14 results, median recall 100.0%, broad saving -101.1%, scoped saving -285.5%, false sufficiency 0.
- `nui_callback`: 28 results, median recall 100.0%, broad saving 83.8%, scoped saving -142.8%, false sufficiency 0.
- `provider_ambiguity`: 14 results, median recall 100.0%, broad saving -48.7%, scoped saving -270.8%, false sufficiency 0.
- `repo_isolation`: 14 results, median recall 100.0%, broad saving -837.5%, scoped saving -837.5%, false sufficiency 0.
- `typescript_api`: 14 results, median recall 100.0%, broad saving -49.8%, scoped saving -390.5%, false sufficiency 0.
- `typescript_feature`: 14 results, median recall 100.0%, broad saving -273.1%, scoped saving -303.9%, false sufficiency 0.
- `verified_cross_resource`: 14 results, median recall 100.0%, broad saving 90.5%, scoped saving -11.6%, false sufficiency 0.
- `verified_cross_resource_call`: 14 results, median recall 100.0%, broad saving 35.2%, scoped saving -239.0%, false sufficiency 0.

## Ground Truth Method

Ground truth is manually authored in `benchmarks/corpus.json`; it is not generated from Planner output. Required items are scored separately from relevant files and unrelated fixture noise.

## Modes Compared

- **manual**: 476 results, median recall 100.0%, median saving 69.3%.
- **panoramic**: 476 results, median recall 100.0%, median saving -30.2%.
- **scoped_panoramic**: 476 results, median recall 100.0%, median saving 69.3%.
- **primitive**: 476 results, median recall 70.8%, median saving 81.1%.
- **phase7**: 476 results, median recall 100.0%, median saving 1.0%.
- **phase7_no_early_stop**: 476 results, median recall 100.0%, median saving -7.4%.

### Manual/Baseline

Deterministic ground-truth minimum files; no model behavior is simulated.

### Panoramic / Repomix-Like

Offline whole-fixture broad-file snapshot. It is not Repomix and no Repomix claim is made.

### Scoped Panoramic

Offline relevant-file snapshot using the same task scope available to every mode.

### Primitive Code-Scale

Existing storage symbol/semantic search, bounded source reads, and bounded relationship traces.

### Phase-7 assemble_context

Production Planner, ContextAssembler, TokenCounter, and Sufficiency path.

### Phase-7 no early stop

Benchmark-only paired run using the same Planner/ranking policy while continuing eligible packing stages. It is not a production MCP mode.

## Retrieval Quality

- Required dependency recall across the complete budget matrix: **84.2% mean / 100.0% median**.
- Precision: **100.0% median**.
- Top-5 recall: **100.0% median**.
- Top-10 recall: **100.0% median**.
- Supported-task acceptance recall is evaluated at each task's maximum requested budget; small-budget omissions remain visible in the raw results.

## Token Efficiency

- Median Phase-7 context tokens: **735**.
- Median reduction versus broad baseline: **1.0%**; supported maximum-budget reduction: **0.5%**.
- Median reduction versus scoped baseline: **-270.2%**; supported maximum-budget scoped reduction: **-287.9%**.
- Distribution against broad baseline: p25 **-48.9%**, p75 **35.4%**, p90 **89.6%**.

## Sufficiency

Sufficient **212**, blocked **234**, indeterminate **30**, false sufficiency **0**, false insufficiency **174**. False sufficiency: all budgets **0**, maximum budget **0**, supported **0**, diagnostic **0**, adversarial **0**. Empty retrievals **46**.

## Early Stop

Median retrieval rounds **1.0**, median source reads **2.0**, median latency **8.8 ms**, p95 latency **33.0 ms**. Paired early-stop totals: **13544 tokens**, **14 source reads**, **586 rounds** avoided.

- `broad_architecture`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `callee_trace`: 14 paired runs, 2016 tokens, 14 source reads, 28 rounds avoided.
- `caller_trace`: 14 paired runs, 504 tokens, 0 source reads, 28 rounds avoided.
- `client_server_side_provider`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `command`: 14 paired runs, 648 tokens, 0 source reads, 42 rounds avoided.
- `custom_framework_operation`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `duplicate_symbol`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `dynamic_provider`: 14 paired runs, 926 tokens, 0 source reads, 42 rounds avoided.
- `exact_semantic_endpoint`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `exact_symbol_lookup`: 28 paired runs, 896 tokens, 0 source reads, 42 rounds avoided.
- `execution_side_provider`: 14 paired runs, 920 tokens, 0 source reads, 42 rounds avoided.
- `external_provider`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `fivem_callback_flow`: 14 paired runs, 768 tokens, 0 source reads, 42 rounds avoided.
- `fivem_event_flow`: 14 paired runs, 948 tokens, 0 source reads, 42 rounds avoided.
- `fivem_export_flow`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `fivem_multi_resource_feature`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `focused_multi_resource_feature`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `framework_facade_chain`: 14 paired runs, 588 tokens, 0 source reads, 28 rounds avoided.
- `generic_refactor`: 14 paired runs, 504 tokens, 0 source reads, 24 rounds avoided.
- `impact_blast_radius`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `known_framework_operation`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `lua_generic_feature`: 14 paired runs, 588 tokens, 0 source reads, 28 rounds avoided.
- `medium_feature`: 14 paired runs, 504 tokens, 0 source reads, 24 rounds avoided.
- `multi_resource_feature`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `narrow_bug_fix`: 14 paired runs, 504 tokens, 0 source reads, 24 rounds avoided.
- `nui_callback`: 28 paired runs, 904 tokens, 0 source reads, 42 rounds avoided.
- `provider_ambiguity`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `repo_isolation`: 14 paired runs, 896 tokens, 0 source reads, 42 rounds avoided.
- `typescript_api`: 14 paired runs, 928 tokens, 0 source reads, 42 rounds avoided.
- `typescript_feature`: 14 paired runs, 502 tokens, 0 source reads, 24 rounds avoided.
- `verified_cross_resource`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.
- `verified_cross_resource_call`: 14 paired runs, 0 tokens, 0 source reads, 0 rounds avoided.

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
- Benchmark scorer defect fixed: relationship recall now requires exact indexed edge kind, endpoint identity, files, and returned endpoint source; calls-versus-references and event-versus-callback regressions pass.
- Benchmark scorer defect fixed: structured symbol/provider scoring no longer accepts source-text substrings, and empty retrieval precision is undefined and excluded from aggregates.
- Benchmark fixture defect fixed: callback flow now models a valid client-to-server callback; the required 512-to-32000 budget matrix with two repeats has zero false sufficiency.
- Phase-7 defect fixed: unique Lua require aliases now resolve dotted module calls while ambiguous and shadowed imports remain unresolved; Lua false-sufficiency matrix rerun passed.
- Phase-7 defect fixed: explicit focused generic symbols can expand past same-source multi-analyzer ambiguity; facade-chain budget matrix rerun passed.
- Phase-7 defect fixed: exact semantic provider operations and incoming impact traces remain incomplete until provider/impact evidence is returned; low-budget framework and blast-radius regressions pass.

## Verification Commands

The benchmark CLI is offline and uses the same repository Go toolchain. Release commands are listed in `benchmarks/README.md`.

## jCodeMunch Final Audit

jCodeMunch was used during development for architecture, caller, dependency, changed-symbol, and blast-radius inspection. It is not imported by the harness or runtime.

## Measured Claims

Only the percentages and counts above are claims, and they are computed from this report's task results. No external-agent success or Repomix claim is implied.

## Remaining Limitations

supported-task median token reduction is below the provisional 50% target. External-agent task execution and Repomix comparison were not part of this offline benchmark.

## Final Recommendation

Phase 7.4 status CONDITIONAL PASS; review the hard-gate and provisional limitations reported above before downstream integration.
