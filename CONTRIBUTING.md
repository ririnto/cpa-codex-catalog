# Contributing

Use Go 1.26.8 or newer in the Go 1.26 toolchain and the Taskfile tasks declared by this repository.
The Go tool directive lets you invoke the pinned Task version with `go tool task`.

Run the focused task for the files you change.

```sh
go tool task test
go tool task vet
go tool task build
```

Run `go tool task check` before handing off a complete change.
It checks formatting, race-enabled tests, static analysis, the native library build, and required host integration.

Prepare the pinned SDK host, plugin, and integration test executable before a focused host run.
Preparation may download dependencies.

```sh
go tool task prepare-native
go tool task integration
```

The integration task runs prepared binaries against versioned synthetic seeds.
Missing binaries or seed files fail the required lane.
Linux requires `unshare`, `ip`, `runuser`, and permission to create a network namespace.
The runner enables only loopback and fails when it cannot establish that isolation.
macOS requires `sandbox-exec` and runs the same fixtures with a profile that permits only loopback networking.
The runner fails if the sandbox tool or profile is unavailable.
The host uses its embedded catalogs and disables remote catalog updates.

Keep tests offline and use disposable synthetic fixtures.
Do not use real Codex accounts, provider credentials, live model caches, or production endpoints in tests.
Do not commit generated plugin binaries, local configuration, catalogs, tokens, or machine-specific paths.

Document changes to catalog input, override, reload, or route behavior in `docs/engineering-contracts.md`.
Keep operator commands and configuration examples in `README.md` and `config.example.yaml`.

## Maintained prose

Use one source line per complete sentence in maintained Markdown, contributor guidance, `AGENTS.md`, and `SKILL.md`.
Separate paragraphs with blank lines and preserve spaces between words.
Do not join independent sentences with semicolons or other punctuation.
Preserve technical meaning, permissions, links, code, metadata, exact quotations, tables, and licenses.
Exclude generated and vendored material from mechanical prose edits.

Prefer `.yaml` when the consuming platform supports it.
Before renaming a YAML file, check platform requirements, references, and consumers.
Keep required `.yml` filenames and record the platform evidence in the change description.
GitHub accepts [workflow files](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax) with either extension.

## Issues and pull requests

Use the repository's bug report or improvement template when opening an issue.
Include a synthetic reproduction for bugs and concrete acceptance criteria for improvements.
Keep credentials, catalog contents, prompt text, private paths, and conversation identifiers out of public reports.

Use the pull request template and link the issue that the change resolves.
Describe the problem, changed behavior or documentation, checks, and material limitations.
Distinguish passing checks, failures, blocked checks, and checks not run.
Reuse passing evidence only while relevant behavior, inputs, configuration, and tools remain unchanged.
Review the entire posted change independently before merging.
Follow current branch protections and required checks without bypassing them.
After merging, verify the named base branch and close the resolved issue.
Remove task-owned branches and temporary files only after checking reachability and recovery needs.

## Commit messages

Separate the subject and body with a blank line.
Explain the reason for the change and its main changes in the body.
Record actual checks and material limitations, including checks not run.
Put each complete sentence on its own source line.
