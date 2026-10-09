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
- Do not add another task runner.

## Verification

- Use focused tests for each changed behavior and run `go tool task check` before delivery.
- Keep tests offline and use mock upstreams for host integration.
- Report the exact checks run and any unavailable host evidence.

## References

- Document external compatibility against release tags or maintained branches.
- Use commit identifiers only when exact source traceability is required by a test.
- Follow [contribution guidance](CONTRIBUTING.md) for maintained prose, templates, commit bodies, and delivery evidence.
