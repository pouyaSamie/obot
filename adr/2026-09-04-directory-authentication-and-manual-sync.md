# 2026-09-04: Directory authentication and manual synchronization

- **Status:** Accepted
- **Date:** 2026-09-04
- **Supersedes:** None
- **Superseded by:** None

## Related issues

None.

## Related ODPs

None. This implementation was requested before an ODP was available; a follow-up ODP should record any future expansion to scheduled synchronization or new directory types.

## Context

Obot previously treated configuration as singular even though Local and LDAP are built-in providers. LDAP also had no way to import and maintain directory users without waiting for interactive login.

## Decision

Authentication providers are independently configured and selected per browser session. LDAP directory import is administrator-triggered: preview stores a ten-minute, database-backed token and apply requires a fresh directory snapshot and identical LDAP configuration revision. LDAP sessions and preview records live in the gateway database; directory disappearance disables the LDAP identity and revokes its sessions without deleting Obot users.

## Rationale

Independent provider selection preserves Local access alongside LDAP and prevents an ambiguous session cookie from being routed to the wrong provider. A preview/apply boundary makes a potentially broad directory update reviewable and prevents an incomplete or changed directory read from disabling accounts. Shared persistence supports multi-replica deployments and immediate session invalidation.

## Consequences

Operators must run synchronization manually and rerun preview after its token expires or the directory/configuration changes. LDAP entries link only by exact normalized email, and group-role reconciliation uses direct LDAP groups with the `ldap/` prefix. No user rows are hard-deleted by directory synchronization.

## References

- `pkg/ldapauth/provider.go`
- `pkg/gateway/client/ldap_sync.go`
- `pkg/proxy/proxy.go`
