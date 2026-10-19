# Creative evaluation

This optional developer tool runs real Writing and Game tasks against synthetic projects. It produces promptfoo comparisons and keeps product evidence for investigating failures. Denova's UI and Agent tool set are unchanged. Evaluation is **manual or separately scheduled**: it is not included in CI, commits, merges, or release gates.

## Setup and manual use

Run from the repository:

```bash
./scripts/eval-creative.sh
```

This loads the saved `tools/creative-eval/run.local.json`, runs all four scenarios once for each saved variant, and exports reports. No flags or grading-key export are needed when the grader references an existing model profile. Missing tool dependencies are installed automatically with the frozen lockfile. Use Node.js 22.22+, pnpm, and the repository's Go toolchain.

For a new checkout, configure it once:

```bash
cp tools/creative-eval/run.example.json tools/creative-eval/run.local.json
```

Edit `run.local.json`:

- `settings_file`: an existing Denova **user** `config.toml` containing model connections/profiles. Paths resolve relative to this JSON file. Set the correct path for your installation, including `.nova` if that is your existing data root. The source file is read only.
- `variants`: uniquely named configurations. `profile_id` optionally selects a model profile from that settings file; otherwise its model defaults apply. `writing_skill` defaults to `novel-lite`. To compare Skill versions, `skills_directory` can select another checkout's `skills` directory. A variant can specify its own `settings_file` for model/context/prompt comparisons.
- `grader.profile_id`: an existing enabled model profile in the first variant's settings file. Its endpoint and key are reused for grading, so no extra API key needs exporting. The example uses the same profile for generation and grading; selecting a different grading profile is supported. Profile grading supports OpenAI Chat Completions and Responses endpoints without custom headers or protocol options. For other routes, replace `grader` with an explicit [promptfoo grading provider](https://www.promptfoo.dev/docs/configuration/expected-outputs/) and reference its key through `apiKeyEnvar`.

The example starts with one variant. Add a second variant to compare configurations in the same report. A Writing Skill change affects Writing cases only; use a different model profile or settings file when comparing Game behavior. Changing runtime source code can be evaluated in separate runs; the report history retains each run and its checkout commit. The tool does not automatically choose or replace an approved baseline.

```bash
./scripts/eval-creative.sh --case writing --repeat 3
pnpm --dir tools/creative-eval run view
```

Flags are optional overrides. The script preserves the caller's working directory: the default configuration is always found inside this checkout; an explicit `--config` resolves relative to the caller's directory, while paths inside the configuration resolve relative to the JSON file. It can also be invoked by absolute path from a separate scheduler without flags.

`--case` matches a case ID substring. `--repeat` generates fresh independent runs; promptfoo response caching is disabled. `run view` starts promptfoo's report viewer (default port 15500; `--port` selects another). Use the evaluation command for reruns: the viewer cannot rebuild the backend or recreate isolated Projects from a saved report alone. Every evaluation also exports standalone `report.html`, `report.json`, and a `run.json` manifest under `tools/creative-eval/results/`.

Generation **and semantic grading** make paid requests when using paid model services. Start with one variant and one repetition. Token usage is reported when Denova provides it. Monetary cost is not calculated by the Denova adapter: any zero-dollar value shown by promptfoo is not a billing total. Provider latency includes backend startup, setup, and shutdown; `execution_duration_ms` in case metadata measures execution and result collection after fixture setup. There is no fixed total Agent runtime limit; Ctrl+C cancels the evaluation and stops its owned backend.

## Scenarios and interpretation

The version-controlled `cases.json` contains four small scenes:

| Case | Product evidence | Semantic question |
| --- | --- | --- |
| `writing-scoped-edit` | Exact replacement, protected files, complete content-file change list | Was only the requested sentence changed? |
| `writing-lore-constraint` | Persisted chapter, Lore, file changes | Does the scene respect the prohibition on creating water? |
| `game-rule-state` | Opening, final narrative and state snapshots | Does the failed action agree with the world's rules and persisted state? |
| `game-branch-isolation` | Alternate branch knowledge, unchanged main branch, main continuation | Does the main character avoid learning another branch's secret? |

Programmatic product checks and model-graded creative checks are separate named metrics. Inspect the original text, grading explanation, and per-case `evidence.json` before drawing conclusions. A failed setup or grader is an execution error, not evidence of poor writing; a missing result is never a pass. Literary quality and disputed judgments need human review. Repetition shows variation but does not establish statistical significance by itself.

Quality failures are advisory and do not set a failing command exit status. Setup/execution/grading errors exit nonzero, and user cancellation exits 130. Scheduling can call the same command and inspect `report.json`; no scheduler or recurring task is installed by this change.

## Isolation and evidence

The runner builds the current backend once. Each case, variant, and repetition owns a new data root, a new Project, and a loopback backend on a free port. It uses ordinary product APIs for setup and execution; it never connects to the development server, imports user books, or copies user sessions. Only model connections/profiles and model/context/prompt options are copied from the source settings. Native execution remains in the existing runtime.

Per-case logs, product journals, snapshots, and evidence are retained under the ignored results directory for diagnosis. The temporary `config.toml` containing model credentials, its initialization backups, and the per-run compiled binary are removed after use. Reports and promptfoo's local history stay on disk; automatic sharing and promptfoo telemetry are disabled. Profile grading passes its key through a temporary process environment variable and clears it after the run. Explicit promptfoo graders should also reference credentials through environment variables, because promptfoo persists grading configuration in reports/history.

Results contain synthetic story content and can be removed when no longer needed. Their paths are developer artifacts, not an alternative Denova recovery ledger or user-data format. Existing Release user directories require no migration.

## Verify the integration without paid calls

```bash
pnpm --dir tools/creative-eval verify
```

This explicit local verification starts a deterministic model fixture with the real backend and promptfoo. It exercises product setup, tool execution, branch isolation, two-variant comparison with a deliberately incorrect edit, report export, credential-backup cleanup, and cancellation during a pending model request. It verifies the harness, not real-model quality, and is not added to CI.
