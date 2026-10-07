# Codex Model Catalog for CLIProxyAPI

This native CLIProxyAPI plugin applies sparse metadata overrides to the host-generated Codex model catalog.
The host controls model membership and preserves its own routing and availability rules.

## Requirements

- Go 1.26.8 or newer.
- CLIProxyAPI release `v8.0.15` or a compatible later release.
- A C toolchain for the native shared library build.
- Codex configured with a provider that supports the Responses API.

The module uses the official CLIProxyAPI SDK v8.0.15.
That release exposes a response interceptor for the generated Codex model list.

## Build and install

Build the plugin for the current operating system and architecture.

```sh
go tool task build
```

The artifact is written to `build/plugins/<GOOS>/<GOARCH>/cpa-codex-catalog.<ext>`.
Copy it to `<plugin-root>/<GOOS>/<GOARCH>/`, where `plugin-root` is CLIProxyAPI's configured plugin directory.
The filename without its platform extension is the plugin ID, `cpa-codex-catalog`.
The host and plugin must use the same operating system and architecture.

Enable the plugin in CLIProxyAPI configuration and place sparse fields under `defaults` or `models`.
Use `config.example.yaml` as a starting point.
The plugin reads no external catalog file and does not create a second model inventory.
The host generates model membership from its configured providers and available credentials.
The native plugin runs inside the CLIProxyAPI process, so install only builds you trust.

The plugin intercepts CLIProxyAPI's generated `/v1/models?client_version=...` Codex response.
Codex must use the proxy's `/v1` base URL and enable model discovery for API-key providers.
Configure the proxy API key through Codex's normal provider authentication.

```toml
model = "example-model"
model_provider = "example"
review_model = "gpt-6-luna"

[features]
api_key_model_discovery = true

[memories]
extract_model = "gpt-6-luna"
consolidation_model = "gpt-6-luna"

[agents]
default_subagent_model = "gpt-6-luna"

[model_providers.example]
name = "Example"
base_url = "http://127.0.0.1:8317/v1"
model_catalog_url = "http://127.0.0.1:8317/v1/models"
wire_api = "responses"
env_key = "CLIPROXY_API_KEY"
```

Restart Codex after changing provider configuration.
The [Z.AI Codex guide](https://docs.z.ai/devpack/tool/codex.md) shows the local `model_catalog_json` option and Responses setup.
Its sample uses the older `base_instructions` field, while current Codex metadata represents prompt text under `model_messages.instructions_template`.
Preserving a legacy field in a catalog does not guarantee that every Codex version will use it.
Codex uses `model_catalog_url` for discovery and `base_url` for Responses requests.
An `openai_base_url` override changes the built-in OpenAI endpoint.
API-key discovery requires an explicit catalog URL when you override that endpoint.
Use the custom provider above to fetch this plugin's catalog.

## Catalog and overrides

`defaults` applies each named field to every model the host generates.
`models` applies fields to an exact model slug after defaults.
The plugin merges nested objects and replaces arrays.
Overrides for unavailable slugs stay dormant and never add catalog entries.
An empty host catalog remains empty.
The plugin preserves host response fields and every model field that an override leaves unspecified.

The official v8.0.15 SDK does not expose the request URL or query to response interceptors.
The plugin identifies Codex catalogs by the OpenAI source format, empty execution fields, and the generated `models[].slug` shape.
Generic OpenAI `data[]` lists and Claude, Gemini, and Grok model lists keep their host formats.
Execution responses and tool payloads with model identifiers bypass catalog overrides.
When an override changes the body, the plugin clears stale `Content-Length` and `ETag` headers.
See [engineering contracts](docs/engineering-contracts.md) for the interception and metadata limits.

```yaml
plugins:
  enabled: true
  dir: plugins
  configs:
    cpa-codex-catalog:
      enabled: true
      defaults:
        model_messages:
          instructions_template: "Complete the requested code change and verify its behavior."
      models:
        example-model:
          display_name: Example Coding Model
```

Use values supported by the model and Codex version.
Configure model behavior in CLIProxyAPI's provider settings when the provider needs matching capabilities.

## Optional local export

Use `catalog-export` when a Codex client needs a local `model_catalog_json` file.
The command builds a complete offline Codex catalog from an existing base and a partial override file.
It refuses to replace an existing output unless you pass `--force`.

```sh
go run ./cmd/catalog-export \
  --base "/path/to/existing/models_cache.json" \
  --overrides "$TMPDIR/overrides.json" \
  --out "$TMPDIR/models.json"
```

Omit `--overrides` when no override file is needed.
The command writes the merged result to the explicit output path and leaves both inputs unchanged.
Review that full output before configuring Codex to use it.
The command does not read or modify Codex's global configuration.

## Verification

Run the repository checks with:

```sh
go tool task check
```

The check prepares the pinned SDK host, native plugin, and integration test binary before running the synthetic host cases.
Dependency preparation may download the modules selected by `go.mod`.
The integration run uses committed seeds under `integration/testdata/v1-synthetic` and requires no provider credentials.
Linux runs it in a network namespace with only loopback available.
On macOS, `sandbox-exec` permits only loopback networking and the runner isolates the child environment.

See [contributing](CONTRIBUTING.md) for focused test and host integration commands.

## References

- [CLIProxyAPI v8.0.15 plugin example](https://github.com/router-for-me/CLIProxyAPI/blob/v8.0.15/examples/plugin/simple/README.md) documents plugin discovery and interceptor capabilities.
- [Codex provider configuration](https://github.com/openai/codex/blob/main/codex-rs/model-provider-info/src/lib.rs) defines provider settings for model discovery and authentication.
- [Codex model catalog client](https://github.com/openai/codex/blob/main/codex-rs/codex-api/src/endpoint/models.rs) fetches model catalogs through the provider client.
- [Codex model metadata schema](https://github.com/openai/codex/blob/main/codex-rs/protocol/src/openai_models.rs) defines current prompt metadata under `model_messages`.
