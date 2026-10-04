# Engineering Contracts

## Catalog input

The base input may be a full Codex `{ "models": [...] }` response or the supported Codex cache wrapper.
Treat the source catalog as the complete model set for this resource route.
Do not fill gaps from the CLIProxyAPI bundled model registry.

The optional override file is a JSON object with `defaults` and `models` maps.
`defaults` applies its model fields to every source model.
`models` maps source slugs to model-field overrides.
Apply defaults first and the matching slug override second.
Merge nested objects by key, and replace arrays as complete values.
Reject duplicate source slugs and override slugs that do not exist in the base catalog.

## Available model intersection

Fetch `available_models_url` for each resource request when configured.
Require the URL path to equal `/v1/models`.
Allow HTTPS.
Allow HTTP only for `localhost` or a loopback IP.
Reject URL userinfo, fragments, redirects, and case-insensitive `client_version` query keys.
Preserve other query parameters.
Validate the environment variable name during configuration.
Read the bearer token named by `available_models_token_env` from the environment on each request.
Return a fixed HTTP 503 response for missing or invalid tokens, malformed inventory, or fetch errors.
Do not expose upstream response bodies, request URLs, or credentials in errors.
Enforce a fixed response-size limit before decoding inventory.
The resource API provides no cancellation context.
Inventory requests have no post-connect timeout.

Parse provider rows from `data[]` and require string `id` fields.
Accept a missing provider `supported_in_api` flag, exclude `false`, and reject other flag types.
Keep a catalog model only when its slug exactly matches a provider ID and its `supported_in_api` value is true.
Match IDs case-sensitively, collapse duplicate provider IDs, and ignore unknown IDs.
Preserve source catalog order and all merged catalog fields.
Do not add provider fields or models to the catalog.
Return HTTP 200 with an empty catalog when valid inventory has no eligible models.
Never serve stale inventory or the unfiltered catalog after an error.

Treat generic CLIProxyAPI `/v1/models` output as host-routable inventory, not per-provider entitlement.
Use provider-filtered discovery when account-specific availability is required.
Do not infer entitlements for providers without authoritative discovery.

An override changes only the fields it names.
Preserve other source fields, including prompt metadata, without rewriting their contents.
Preserved unknown or legacy fields are not guaranteed to affect Codex behavior.
Current Codex prompt metadata uses `model_messages.instructions_template`.
Do not write account identity or cache fetch-time metadata into the served catalog.

## Runtime state

Parse and validate a candidate catalog before publishing it to the resource handler.
Each successful configure or reconfigure operation publishes one complete snapshot.
Resource requests read one snapshot and do not observe a partial update.
If a candidate is invalid, retain the last valid active snapshot.

The resource route is `GET /v0/resource/plugins/cpa-codex-catalog/models`.
Return the full merged catalog unless availability filtering is configured.
Apply the exact provider-ID intersection after merging catalog overrides.
The standard CLIProxyAPI `/v1/models` response is a separate API surface with its own fields.
CLIProxyAPI Home mode currently returns 404 for this plugin resource route.

## Provider limits

The selected provider remains a CLIProxyAPI routing choice and is not selected per catalog model.
Catalog metadata describes model capabilities to Codex but cannot add missing upstream behavior.
The plugin does not currently support an override for parallel tool-call capability.

## Authentication

Treat catalog metadata as visible to every client that can reach the resource route.
`bearer_token_env` names an environment variable whose value protects the resource route.
When unset, the route is public to clients that can reach the listener.
When configured, require an exact bearer token match and return HTTP 401 with `WWW-Authenticate: Bearer` for other requests.
Codex can send the provider credential when it fetches the configured `model_catalog_url`.
Use the same environment variable name in Codex provider `env_key` and in the plugin's `bearer_token_env`.
Store the token value outside repository files and never include it in logs or diagnostics.

## Configuration paths

`catalog_path` is required when the plugin is enabled.
Resolve relative catalog and override paths from the CLIProxyAPI host process working directory.
`overrides_path`, `bearer_token_env`, `available_models_url`, and `available_models_token_env` are optional.

## Local export

The `catalog-export` command reads only the paths passed through its flags.
It does not search for or modify Codex global configuration.
It must refuse to replace an existing output unless the caller passes `--force`.
The export should include only model catalog data and omit cache identity and fetch-time fields.
