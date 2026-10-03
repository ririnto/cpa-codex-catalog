# Catalog Delivery Plan

## Outcome

Serve full Codex model metadata through a CLIProxyAPI native resource route.
Preserve base metadata and apply explicit per-model overrides for capabilities, reasoning, context, and prompts.
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
The working branch is `codex/catalog-customization` and the target branch is `main`.
Use named branches and release tags for durable references.

## Acceptance

Validate full catalog fields against the current Codex schema and keep explicit values unchanged.
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
- Ready for pull request integration into `main`.
