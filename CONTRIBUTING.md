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
It checks formatting, race-enabled tests, static analysis, and the native library build.

The integration task requires `CPA_BINARY` to point to a CLIProxyAPI v8 server binary.
It runs against a mock upstream and should use synthetic catalog files.

```sh
CPA_BINARY=/path/to/cli-proxy-api go tool task integration
```

Run host integration outside CLIProxyAPI Home mode because Home mode does not serve plugin resources.

Keep tests offline and use disposable synthetic fixtures.
Do not use real Codex accounts, provider credentials, live model caches, or production endpoints in tests.
Do not commit generated plugin binaries, local configuration, catalogs, tokens, or machine-specific paths.

Document changes to catalog input, override, reload, or route behavior in `docs/engineering-contracts.md`.
Keep operator commands and configuration examples in `README.md` and `config.example.yaml`.
