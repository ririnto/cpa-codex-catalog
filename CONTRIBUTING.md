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
