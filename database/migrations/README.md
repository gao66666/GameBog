# Database Migrations

This project uses SQL migration files under this directory.

## Naming
- Use incremental prefixes, for example: `000002_add_article_index.sql`
- Keep every migration idempotent when possible.

## Recommended tool
- `goose` (https://github.com/pressly/goose)

## Workflow
1. Create a new migration file.
2. Add both `Up` and `Down` sections.
3. Run migration in dev/staging before production.
4. Attach migration id in release notes.
