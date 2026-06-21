---
name: deep-analysis
description: Use when Codex should consult the Deep Analysis CLI (`deep-analysis`) from outside the Deep Analysis source checkout for expensive, systematic analysis of markdown prompts, codebases, design docs, architecture questions, debugging investigations, PR/design reviews, or hard tradeoff analysis where a second AI researcher with file discovery is useful. Do not use for work inside the `github.com/lox/deep-analysis` repository itself, trivial searches, small edits, or questions that can be answered directly with ordinary local tools.
---

# Deep Analysis

## Overview

Use Deep Analysis as a second-pass researcher for hard questions in another repository or document workspace. The CLI reads a markdown task, explores files under a target working directory with its own `find_files`, `summarize_files`, and `read_files` tools, appends an analysis section to a markdown output file, and logs session and cost information to stderr.

It cannot run tests, browse the web, inspect live services, or change target project files. It does write the requested analysis output file. Use normal Codex tools for direct verification, and treat Deep Analysis output as evidence to verify rather than an instruction to apply blindly.

## Workflow

1. Do a quick local orientation first. Identify the repo or document scope, the concrete question, important constraints, and any current facts the Deep Analysis run should know.
2. If the current workspace is the Deep Analysis source checkout, stop and use ordinary local tools unless the user explicitly asked to run Deep Analysis against a different target.
3. Confirm `deep-analysis` is available. If not, install it with `mise use -g github:lox/deep-analysis`; if the current shell still cannot find it, invoke it through `mise exec -- deep-analysis`.
4. Confirm `OPENAI_API_KEY` is set without printing its value. If it is missing or the CLI is unavailable, report that blocker instead of inventing an analysis.
5. Write a concise markdown prompt. Include the task, target repo path, context already discovered, files or directories worth considering, constraints, and the desired output shape.
6. Run the CLI with an explicit `--output` path unless the user specifically wants the input document updated in place.
7. Read the generated output, then verify any actionable claims against the repo before editing code or reporting conclusions.

## Running The CLI

Use absolute paths when combining `--cwd` with a task document outside the target repo. The CLI changes into `--cwd` before resolving relative input and output paths.

```bash
deep-analysis --cwd /path/to/repo /absolute/path/task.md --output /absolute/path/analysis.md
```

If `deep-analysis` is not available:

```bash
mise use -g github:lox/deep-analysis
deep-analysis --help
```

If the current process cannot see the newly installed command yet, use `mise exec -- deep-analysis ...`.

Defaults:

- Models: use the CLI defaults, which track the latest intended models; do not pin model versions unless the user explicitly asks.
- Reasoning effort: `xhigh`
- Output path: input file, when `--output` is omitted
- Sessions: `~/.local/state/deep-analysis/sessions/`

Use `--reasoning-effort high` or `medium` when speed or cost matters more than maximum depth.

## Prompt Shape

Prefer a task document like this:

```markdown
# Task

Answer the specific question or perform the requested review.

# Target

- Repo: /absolute/path/to/repo
- Relevant paths: path/one, path/two

# Context Already Gathered

- Concrete fact or command output summary.
- Constraint from the user.

# What To Produce

- Findings with file references.
- Risks and confidence.
- Recommended next steps.
```

Keep the prompt focused. Deep Analysis is strongest when it gets a sharp question and enough context to avoid re-discovering basics.

## Continuations

The CLI logs a session id when it saves state:

```text
INFO Saved session session=<session-id> response_id=<response-id>
```

For a follow-up, add the new question to the prior analysis document or a new task document that includes the relevant previous context, then run:

```bash
deep-analysis --cwd /path/to/repo /absolute/path/follow-up.md --output /absolute/path/follow-up-analysis.md --continue <session-id>
```

Use `--reset` with the same session id only when intentionally starting fresh.

## Interpreting Results

- Read the output file before responding to the user.
- Summarize the useful conclusions; do not paste the whole analysis unless asked.
- Re-check code, tests, logs, or live state yourself before making changes based on Deep Analysis recommendations.
- Mention material uncertainty, model/API failures, or unexpectedly high cost when it affects the answer.

## Common Failures

- `OPENAI_API_KEY environment variable is required`: ask the user to provide credentials or skip the Deep Analysis run.
- `input file not found` with `--cwd`: use absolute task and output paths, or place the task document inside the target working directory.
- Model access, quota, or rate-limit errors: report the provider error and continue with ordinary local analysis where possible.
