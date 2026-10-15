# Contributing

Use Go 1.26.9 or newer in the Go 1.26 toolchain and the Taskfile tasks declared by this repository.
The Go tool directive lets you invoke the pinned Task and govulncheck versions with `go tool`.

Use Node.js 24 LTS and the pnpm version declared in `package.json` for contributor tooling.
Prefer supported LTS releases when selecting toolchains and dependencies, or stable releases when no LTS line exists.
Define pnpm dependency versions as `^x.y.z` ranges in `pnpm-workspace.yaml` and reference them with `catalog:`.
Use overrides only for necessary security fixes and document the advisory, affected dependency path, and removal condition.
Do not add version-plus-hash pins or remove lockfile integrity checks.
Install the locked dependencies and Git hooks from the repository root.

```sh
pnpm install --frozen-lockfile
pnpm run check
pnpm run fix
```

The root scripts compose TypeScript, Ultracite, Markdownlint, and the existing Go Taskfile checks.
`pnpm run check` runs the complete native verification and the focused TypeScript, lint-rule, and tooling checks.
`pnpm run fix` applies Markdownlint fixes, Ultracite formatting, and Go formatting.
The Markdown checks enforce compact tables and the repository's sentence, table-separator, and box-drawing rules.
Review prose diagnostics manually because sentence recognition has bounded coverage.
Keep each sentence on its own source line and avoid semicolon joins and em dashes.
Run `pnpm run check:lint-rules` to test custom Markdown rule behavior with synthetic input.
Run `pnpm run check:typescript` to type-check the custom rules and tooling configurations.
Run `pnpm run check:ultracite` and `pnpm run fix:ultracite` for JavaScript and TypeScript linting and formatting.
Ultracite is the only configured caller of Oxlint and Oxfmt.

Keep the `prepare` script invoking Husky so dependency installation prepares real Git hooks for this clone.
The pre-commit hook invokes `pnpm run pre-commit`.
The pre-push hook invokes `pnpm run pre-push`.
Both scripts run all checks and propagate failures to Git.
Keep existing hook owners when composing future hooks.
Invoke Oxlint and Oxfmt only through Ultracite when JavaScript or TypeScript linting or formatting is needed.
Keep lint rules and configuration intact without suppressions or disabled rules.

## Dependency security overrides

`markdownlint-cli2>smol-toml` uses `^1.9.1` to fix [GHSA-r4xh-jqrq-34v2](https://github.com/advisories/GHSA-r4xh-jqrq-34v2).
`micromark-extension-math>katex` uses `^0.19.0` to fix [GHSA-238p-pmpm-9mq7](https://github.com/advisories/GHSA-238p-pmpm-9mq7).
Remove each override when the upstream dependency constraints require a patched release and the lockfile resolves it.

## Native verification

Run the focused task for the files you change.

```sh
go tool task test
go tool task vet
go tool task vuln
go tool task build
```

Run `go tool task check` before handing off a complete change.
It checks formatting, known vulnerabilities, race-enabled tests, static analysis, the native library build, and required host integration.
The vulnerability scan queries the Go vulnerability database and requires network access.
Unit tests and prepared synthetic native integration use local fixtures and do not query that database or provider endpoints.

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
Assert behavior and contracts without hard-pinning prose wording in tests.
Use documentation comments only, preserving language directives and other required semantic comments.
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
If the required independent context cannot read its assignment, record the transport failure and follow explicit delivery authority.
Review the complete current posted diff yourself only when that fallback is authorized.
Follow current branch protections and required checks without bypassing them.
After merging, verify the named base branch and close the resolved issue.
Remove task-owned branches and temporary files only after checking reachability and recovery needs.
Check active processes and handles before deleting task-owned build or verification resources.

## Commit messages

Separate the subject and body with a blank line.
Explain the reason for the change and its main changes in the body.
Record actual checks and material limitations, including checks not run.
Put each complete sentence on its own source line.
