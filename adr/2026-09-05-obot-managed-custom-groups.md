# 2026-09-05: Obot-managed custom groups

- **Status:** Accepted
- **Date:** 2026-09-05
- **Supersedes:** None
- **Superseded by:** None

## Related issues

None.

## Related ODPs

None. The design was approved in this task; a follow-up ODP is required before extending this model to automatic directory-to-custom-group mapping or policy denies.

## Context

Directory groups describe identity-provider membership, while organizations also need stable, administrator-managed business groups. Those groups must work consistently for Local and LDAP identities without LDAP synchronization overwriting manual assignments.

## Decision

Obot-managed groups use immutable `custom/` IDs and user-based memberships in the gateway database. A protected `system/ldap-users` group is evaluated dynamically from active LDAP identities. Both group types are injected into the existing authorization subject context; provider-owned group persistence and cleanup remain unchanged.

## Rationale

A separate identifier namespace prevents auth-provider cleanup from modifying manual membership. A computed LDAP population avoids copying every synchronized account into a group that could become stale. Reusing the existing subject context lets model, MCP, and hosted-agent access policies consume custom groups without introducing another policy engine.

## Consequences

Administrators can manage team membership independently of LDAP and can use those teams in existing policy forms. Custom group changes are effective on subsequent authenticated requests. Automatic mappings from LDAP groups to custom groups are intentionally out of scope.

## References

- `pkg/gateway/client/customgroup.go`
- `pkg/gateway/types/group.go`
- `ui/user/src/routes/admin/custom-groups/+page.svelte`