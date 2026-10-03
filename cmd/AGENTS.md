# Command Instructions

- Keep the shared library name and plugin ID aligned with the documented resource route.
- Preserve the CLIProxyAPI SDK ABI and native entry points pinned by `go.mod`.
- Require explicit input and output paths for catalog export.
- Refuse to replace an existing export unless the caller passes `--force`.
- Read only catalog paths supplied by the caller.
- Keep errors free of catalog values, prompt text, credentials, and local file contents.
- Update the operator examples when flags or output behavior change.
