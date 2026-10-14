# Engineering Instructions

## Privacy

- Treat catalog files, prompt text, and provider credentials as user data.
- Use synthetic fixtures and examples.
- Access live catalogs and local configuration only within the user's authorized operator scope.
- Keep operator data and secrets out of publications.
- Never write Codex global configuration from this repository.
- Treat served model metadata as visible to every client that can reach the route.
- Configure bearer authentication when access must be restricted.
- Preserve existing Git author and committer identities when committing or reviewing changes.

## Implementation

- Keep the native plugin compatible with the CLIProxyAPI SDK release pinned in `go.mod`.
- Use the existing Taskfile and `go tool task`.
- Keep native Go tasks in the Taskfile and compose them through the root pnpm scripts.
- Keep the shared Markdown checks aligned with the applicable Sinon Harness tooling baseline.

## Development lifecycle

- Define the observable outcome and affected contracts before implementation.
- Stop discovery when evidence resolves the implementation decision.
- Reproduce defects with synthetic inputs and fix the demonstrated cause.
- Remove superseded interfaces and update their consumers together.
- Encode recurring mistakes in existing checks before adding more instructions.
- Prove new checks reject representative invalid inputs and accept valid inputs.
- Run checks on the same boundary that demonstrated the defect.
- Keep native integration evidence separate from live provider or installed-host acceptance.

These rules apply the relevant [pstack lifecycle principles](https://github.com/cursor/plugins/tree/main/pstack).

## Verification

- Use focused tests for each changed behavior and run `pnpm run check` before delivery.
- Keep `go tool task check` as the complete native verification owner.
- Keep tests offline and use mock upstreams for host integration.
- Report the exact checks run and any unavailable host evidence.

## References

- Document external compatibility against release tags or maintained branches.
- Use commit identifiers only when exact source traceability is required by a test.
- Follow [contribution guidance](CONTRIBUTING.md) for maintained prose, templates, commit bodies, and delivery evidence.
