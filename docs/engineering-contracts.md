# Engineering Contracts

## Runtime model catalog

CLIProxyAPI generates the model catalog from configured providers and available credentials.
The generated catalog controls model membership and order.
The plugin does not add, remove, or reorder models.
An empty generated `models` array remains empty.

The plugin applies `defaults` to each generated Codex model.
It applies an exact-slug entry from `models` after `defaults`.
Merge nested objects recursively, and replace arrays as complete values.
Preserve every host response field that the plugin does not change.
Preserve every model field that an override does not change.
Ignore override slugs absent from the generated catalog.
Absent slugs remain dormant and do not create models.
Set `enabled: true` to activate the plugin.
Reconfiguration validates a complete candidate before publishing it atomically.
Invalid reconfiguration keeps the last valid settings active.

## Interceptor selection

The plugin uses the CLIProxyAPI v8.0.15 response interceptor.
The SDK does not expose the request URL or query string to this interceptor.
The plugin selects status 200, non-stream responses with the `openai` source format.
It also requires empty execution model fields and empty request bodies.
For non-empty catalogs, each row must contain `slug`, `model_messages`, and `supported_reasoning_levels`.
An empty `models` array is also accepted as a generated Codex catalog.
Generic OpenAI `data[]` inventories keep their original response.
Claude, Gemini, and Grok model inventories keep their original response.
Execution and tool responses keep their original response.
When a patch changes the body, the plugin clears `Content-Length` and `ETag`.

The SDK selection fields are the available distinction between model lists and other responses.
Review this gate if a future SDK adds a dedicated model-list marker.

## Model metadata

Model overrides change only fields that they name.
Preserve host prompt metadata unless an override names those fields.
Current Codex prompt metadata uses `model_messages.instructions_template`.
Legacy fields may be preserved without affecting Codex behavior.
Metadata cannot enable behavior that the selected provider does not support.

## Offline export

The `catalog-export` command reads only input paths passed through its flags.
It does not read or modify Codex global configuration.
The command requires explicit base and output paths.
It refuses to replace an existing output unless the caller passes `--force`.
An optional override file uses the same `defaults` and `models` structure.
Export rejects duplicate source slugs and override slugs absent from the base catalog.
Export output contains only the `models` catalog field.
It omits cache identity and fetch-time fields from wrapper responses.
