# Troubleshooting

When something goes wrong, find the page for the situation, then the section for the symptom. Every section has the same
form:

- **Symptom**: what you see — an error, a config that didn't take effect, an address that can't be reached.
- **Cause**: why it happens.
- **Fix**: what to change.

| Situation | Page |
| --- | --- |
| `brickkit up` / `down` fails, components won't start | [up / down problems](01-up-down-issues.md) |
| Debugging a component in your IDE, processes on this machine | [Local debugging problems](02-local-debug-issues.md) |
| Conflict blocks in `config/`, `$var:` not found, config not taking effect | [Config conflict problems](03-config-conflict-issues.md) |
| Fetching a component from a Git repository fails | [Git authentication problems](04-git-auth-issues.md) |
| `brickkit build`, images | [Build problems](05-build-issues.md) |
| Shells and members | [Shell problems](06-shell-issues.md) |
| Database migrations | [Migration problems](07-migration-issues.md) |
| `brickkit lint`, JSON Schemas in your editor, the component table in `AGENTS.md` | [lint and schema problems](08-lint-and-schema-issues.md) |

**When the error carries an error code**, check [Error codes](../06-architecture/09-error-codes.md) first: it lists, by
error code, every error title the CLI prints, and searching that page for the title finds the cause and the fix. This
module covers what error codes can't — cases with no error but a wrong result, and the longer story behind an error.
