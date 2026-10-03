# Runtime Instructions

- Build a complete candidate snapshot before replacing the active catalog.
- Keep the active snapshot unchanged when parsing or validation fails during reconfiguration.
- Reject duplicate source slugs and overrides for unknown slugs.
- Apply defaults before slug-specific overrides, and replace arrays instead of concatenating them.
- Preserve unspecified source fields without implying that Codex consumes unsupported or legacy fields.
- Preserve current Codex prompt metadata under its supported `model_messages.instructions_template` schema.
- Keep the configured catalog authoritative and do not restore bundled models as a fallback.
- Keep cache identity and fetch-time fields out of the served catalog.
- Do not claim that a model declaration enables behavior unsupported by the selected upstream provider.
- Keep detailed input, reload, and metadata behavior aligned with `docs/engineering-contracts.md`.
