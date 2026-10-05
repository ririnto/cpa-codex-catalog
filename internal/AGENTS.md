# Runtime Instructions

- Build and validate a complete candidate override snapshot before replacing the active configuration.
- Keep the active snapshot unchanged when parsing or validation fails during reconfiguration.
- Keep host-generated model membership authoritative and ignore overrides for absent slugs.
- Apply defaults before exact-slug overrides, and replace arrays instead of concatenating them.
- Preserve top-level host fields and every model field that an override does not name.
- Preserve Codex prompt metadata under its supported `model_messages.instructions_template` schema.
- Keep offline export strict about duplicate source slugs and unknown override slugs.
- Remove cache identity and fetch-time fields from offline exports.
- Do not claim that a model declaration enables behavior unsupported by the selected upstream provider.
- Keep detailed input, reload, and metadata behavior aligned with `docs/engineering-contracts.md`.
