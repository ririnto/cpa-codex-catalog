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

Main owns repository setup, dependencies, docs, integration, commits, publication, and merge.
The catalog engine and native adapter have separate writers.
The current follow-up is documentation-only and does not change source code or tests.
The working branch is `codex/partial-catalog-workflow` and the target branch is `main`.
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
- The current branch documents the read-only base and partial-override workflow.
