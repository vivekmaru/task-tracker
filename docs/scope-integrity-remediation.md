# Scope integrity remediation

Migration `0013_scope_integrity` enforces that records with repeated workspace,
project, ticket, and attempt identifiers all describe the same scope. It never
reassigns records automatically.

If the migration reports a preflight failure, run the read-only inventory:

```bash
psql "$FORGE_DATABASE_URL" -f sql/scope-integrity-inventory.sql
```

The result contains relationship names, counts, and at most 20 row IDs per
relationship—never ticket or artifact contents. Correct each row deliberately
by moving it to its real scope, removing an invalid reference, or deleting an
invalid duplicate. Back up the database first, then rerun `forge migrate`.

Do not disable constraints or use `session_replication_role` in production to
work around the preflight: that would recreate the ownership ambiguity the
migration is intended to eliminate.
