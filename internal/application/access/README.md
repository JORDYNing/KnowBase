# Resource access

`access` resolves resource permissions independently of HTTP routing.

- `ResolveKB` accepts only a server-loaded knowledge base in the authenticated caller's tenant. It checks API-key knowledge-base scope before producing a grant.
- A `KBAccess` context grant binds the original caller, exact knowledge-base ID, authoritative owner tenant and operation permission. Changing the execution tenant does not change the caller or grant any permission.
- `KBPermissions` accepts tenant-owned reads and exact upstream grants. There is no cross-space membership, resource-sharing lookup or agent fallback.
- `RequireKBWrite` consumes an admitted write grant and applies the API-key ingest capability. `WithKBTaskWrite` carries one exact knowledge base into trusted background work without creating a user or administrator.
- Clone and move operations use an exact source/destination/operation/task grant. Source and destination must belong to the same tenant; task continuation retains that pair and validates backend compatibility.
- `ResolveKBFile` requires an exact read grant, a matching physical owner and a live knowledge-base resource binding. Merely mentioning a handle does not make it a knowledge-base attachment.
- Message file and artifact downloads require a session-authorized persisted message, an exact output reference and caller-tenant file ownership. Foreign resources fail closed even when their handles appear in historical messages. Session-owned generated artifacts remain available independently of the agent that produced them.
- Deleted artifact tombstones cannot be bypassed through old answer text. Signed public resource grants and independent file-owner checks retain their own authentication contracts.

Tenant member roles, resource creator restrictions, API-key capabilities and resource grants are separate checks. Callers must complete route-level admission before handing a grant to a service. Internal workers must validate the persisted resource binding and expected tenant before requesting a task grant.

The package tests cover owned access, foreign-tenant rejection, caller and resource substitution, execution-scope isolation, API-key scope, read/write grant separation, transfer pairing and message-file ownership.
