# Catalog Delivery Plan

## Outcome

Serve full Codex model metadata through a CLIProxyAPI native resource route.
Use an existing cache or catalog as a read-only base and apply only sparse operator-authored overrides.
Return the full merged model response required by Codex while keeping the override file partial.
Provide a local export option for Codex installations that support `model_catalog_json`.

## Scope

The project owns native plugin code, catalog validation and merge logic, a local export command, tests, and portable documentation.
Existing Codex and CLIProxyAPI checkouts are read-only references.
Do not change global Codex settings or publish private catalog files.
Use local mock data and isolated host integration for acceptance.
Private publication, focused issue tracking, pull request review, and merging into `main` are authorized.

## Ownership and Delivery

Main owns the plan, configuration, commits, publication, and merge.
One implementation owner maintains the catalog engine, native adapter, and integration tests for this unit.
The documentation owner maintains operator examples and runtime contracts.
The working branch is `codex/available-model-intersection` and the target branch is `main`.
Use named branches and release tags for durable references.

## Acceptance

Load an existing read-only `models_cache.json` wrapper or full catalog and preserve unspecified metadata.
Validate partial overrides against the current Codex schema and keep explicit values unchanged.
Return the full merged `{ "models": [...] }` response from the resource route.
Reject malformed inputs, duplicate model IDs, unsafe model overrides, and invalid model capability values.
Test concurrent reads and reconfiguration with synthetic catalogs.
Load the native artifact in an isolated CLIProxyAPI host and request the resource route.
Run formatting, race tests, vet, and native builds on the supported Go toolchain.
Review the published pull request independently before merging.

## Progress

- Implemented the native resource route, metadata overrides, validated reloads, and private local export.
- Validated known nested metadata against the current Codex types while preserving unknown fields.
- Passed formatting, race tests, vet, and native builds on Go 1.26.8.
- Passed native host integration for bearer protection, metadata overrides, and wrapper removal.
- Confirmed authenticated remote catalog consumption and rich metadata preservation with Codex CLI 0.160.0.
- Completed independent review and focused blocker reassessment with no remaining blockers.
- Merged the initial implementation into `main`.
- Documented the read-only base and partial-override workflow.
- Released export input-alias protection in `v0.1.1` after independent review.

## Available Model Intersection

Filter merged metadata against an authenticated model ID endpoint when configured.
Use the host's ordinary OpenAI-compatible model list without a Codex client-version query.
Preserve exact matching slugs, source order, defaults, overrides, and remaining field values.
Exclude metadata marked unsupported for API access.
Return an empty catalog for a valid empty inventory.
Return a safe error when inventory retrieval or validation fails.
Do not fall back to stale or unfiltered metadata.
Keep inventory bodies and credential values outside storage and logs.
The host inventory describes routed models and does not prove every provider's live entitlement.
Provider-specific discovery owns account policy and capability validation.
Verify focused intersection, transport, resource, and native host cases before publishing.
Main verifies isolated authenticated catalog consumption against the filtered host inventory.

## Current Acceptance

Passed Go 1.26.8 formatting checks, package race tests, vet, and native shared-library build.
Passed native host resource authentication, metadata overrides, and authenticated inventory intersection tests.
Confirmed the isolated authenticated resource preserves merged metadata while filtering by exact host model IDs.
Confirmed Codex consumes the filtered catalog and preserves every supplied metadata field.
Codex adds its derived `base_instructions` field when rendering the catalog.
Private catalog values, credentials, and consumer output remain outside publications.
