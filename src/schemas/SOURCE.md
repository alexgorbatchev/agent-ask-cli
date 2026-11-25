# Claude settings validation

`claude-settings.json` is the Claude Code settings schema published at https://json.schemastore.org/claude-code-settings.json, downloaded on 2026-10-23. Claude's settings documentation links this schema: https://code.claude.com/docs/en/settings#edit-a-settings-file.

The upstream Apache-2.0 license is included in `LICENSE`. The schema is unmodified on disk. The wrapper closes the root object during validation so misspelled top-level settings cannot silently disappear in native print mode. Update this snapshot when supporting new native settings, and exercise actual configuration acceptance/rejection in tests. Validation is fully offline.

# Antigravity settings validation

`agy-settings.json` is the Google Antigravity CLI settings schema published at https://antigravity.google/schemas/v1/settings.json, downloaded on 2026-11-22. Antigravity's CLI reference links this schema: https://antigravity.google/docs/cli/reference.

The schema is unmodified on disk. The wrapper closes the root object during offline validation before launch so misspelled or invalid top-level settings fail fast before launching `agy`. Update this snapshot when supporting new native settings.
