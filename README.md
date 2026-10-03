# Codex Model Catalog for CLIProxyAPI

This native CLIProxyAPI plugin serves the full Codex model catalog from an HTTP resource route.
It preserves source metadata and applies explicit JSON overrides without editing Codex's global configuration.

## Requirements

- Go 1.26.8 or newer.
- CLIProxyAPI v8.0.13.
- A C toolchain for the native shared library build.
- Codex configured with a provider that supports the Responses API.

The plugin uses the CLIProxyAPI v8.0.13 SDK pinned in `go.mod`.

## Build and install

Build the plugin for the current operating system and architecture.

```sh
go tool task build
```

The artifact is written to `build/plugins/<GOOS>/<GOARCH>/cpa-codex-catalog.<ext>`.
Copy it to `<plugin-root>/<GOOS>/<GOARCH>/`, where `plugin-root` is CLIProxyAPI's configured plugin directory.
The filename without its platform extension is the plugin ID, `cpa-codex-catalog`.
The host and plugin must use the same operating system and architecture.

Enable the plugin in CLIProxyAPI configuration and set `catalog_path` to a readable JSON catalog.
Use `config.example.yaml` as a starting point and provide local catalog files before enabling the plugin.
Relative catalog paths resolve from the CLIProxyAPI process working directory.
The native plugin runs inside the CLIProxyAPI process, so install only builds you trust.

The plugin serves the catalog at:

```text
GET /v0/resource/plugins/cpa-codex-catalog/models
```

Codex accepts a remote catalog URL through `model_catalog_url` on its provider configuration.
Point it to the resource route and configure the provider's normal API key through `env_key`.
Codex uses that provider authentication when it fetches the catalog.
If `bearer_token_env` is set in the plugin configuration, use the same environment variable name for `env_key`.
When configured, the route accepts only an exact `Authorization: Bearer <token>` value and rejects other requests with HTTP 401.
Without `bearer_token_env`, the route is public to clients that can reach the CLIProxyAPI listener.
Keep the token value in the environment or a secret manager, not in either configuration file.

```toml
model = "example-model"
model_provider = "example"

[model_providers.example]
name = "Example"
base_url = "http://127.0.0.1:8317/v1"
wire_api = "responses"
model_catalog_url = "http://127.0.0.1:8317/v0/resource/plugins/cpa-codex-catalog/models"
env_key = "CODEX_CATALOG_TOKEN"
```

Restart Codex after changing provider configuration.
The [Z.AI Codex guide](https://docs.z.ai/devpack/tool/codex.md) shows the local `model_catalog_json` option and Responses setup.
Its sample uses the older `base_instructions` field, while current Codex metadata represents prompt text under `model_messages.instructions_template`.
Preserving a legacy field in a catalog does not guarantee that every Codex version will use it.
Codex defines `model_catalog_url` for a remote catalog while keeping inference routing on `base_url`.

## Catalog and overrides

The plugin instance defaults to enabled when the host loads native plugins.
`catalog_path` is required and accepts a full `{ "models": [...] }` catalog or the supported Codex cache wrapper as its base.
`overrides_path` is optional and points to a JSON object with optional `defaults` and `models` maps.
Defaults apply to each source model before any matching slug override.
Object fields merge recursively, and arrays replace the base array.
Unknown model slugs and duplicate source slugs fail configuration.
See [engineering contracts](docs/engineering-contracts.md) for reload, fallback, and metadata limits.

The catalog is authoritative for this resource route.
The plugin does not add missing models from CLIProxyAPI's bundled model list.

A metadata override file can set reasoning choices and prompt text for a source model.
The following example uses a synthetic model slug.

```json
{
  "models": {
    "example-model": {
      "display_name": "Example Coding Model",
      "default_reasoning_level": "high",
      "supported_reasoning_levels": [
        {"effort": "low", "description": "Light reasoning"},
        {"effort": "high", "description": "Detailed reasoning"}
      ],
      "model_messages": {
        "instructions_template": "Complete the requested code change and verify its behavior."
      }
    }
  }
}
```

Use values that the model and your Codex version support.
This route supplies an authoritative full catalog to Codex and replaces its bundled catalog fallback.

## Local export

The `catalog-export` command builds an offline Codex catalog from explicit input paths.
It refuses to replace an existing output unless you pass `--force`.

```sh
go run ./cmd/catalog-export \
  --base "$TMPDIR/base.json" \
  --overrides "$TMPDIR/overrides.json" \
  --out "$TMPDIR/models.json"
```

Omit `--overrides` when no override file is needed.
Use an explicit output path and review it before configuring Codex to use it.
The command does not read or modify Codex's global configuration.

## Verification

Run the repository checks with:

```sh
go tool task check
```

See [contributing](CONTRIBUTING.md) for focused test and host integration commands.

## References

- [CLIProxyAPI v8.0.13 plugin example](https://github.com/router-for-me/CLIProxyAPI/blob/v8.0.13/examples/plugin/simple/README.md) documents plugin discovery and resource routes.
- [Codex provider configuration](https://github.com/openai/codex/blob/main/codex-rs/model-provider-info/src/lib.rs) defines `model_catalog_url` and provider authentication fields.
- [Codex model catalog client](https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/endpoint/models.rs) fetches the configured catalog through the provider client.
- [Codex model metadata schema](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/openai_models.rs) defines current prompt metadata under `model_messages`.
