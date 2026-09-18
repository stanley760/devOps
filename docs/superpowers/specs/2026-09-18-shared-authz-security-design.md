# Shared Authentication and Authorization Design

## Status

The design was approved in chat on 2026-09-18. This spec must be reviewed before
implementation starts.

## Goal

Extract reusable authentication and authorization capabilities from `devops-system`
into `devops-infra`, so future business services can share:

- the existing access/refresh token and Redis session protocol;
- RS256 access-token verification compatible with Higress and JWKS key rotation;
- the existing tenant-domain Casbin policy model and `casbin_rule` storage;
- framework-independent authentication and authorization decisions;
- Gin adapters that preserve each service's HTTP response format.

The first consumer remains `devops-system`. Its management-plane roles, permission
names, tenant lifecycle checks, and tenant database context remain business-owned.

## Decisions and constraints

- Existing refresh session IDs, Redis keys, and Casbin rows remain compatible. The
  access-token signing algorithm changes from HS256 to RS256, so existing HS256
  access tokens are intentionally invalidated at rollout. Refresh sessions remain
  valid and can issue new RS256 access tokens.
- Higress validates the RS256 access-token signature, while each backend validates
  the same token again and checks Redis session state. Backend authorization never
  trusts identity headers supplied by the gateway.
- The signing private key is held by configuration/secrets management. Services and
  Higress receive public keys through JWKS. Access tokens include `kid` so multiple
  public keys can coexist during rotation.
- The initial implementation is a shared Go package, not a standalone authentication
  or authorization service.
- Core security decisions are framework-independent. Gin integration is an adapter.
- Public APIs return Go errors and typed results; the infrastructure package does not
  depend on `devops-system` response formatting.
- Authentication and session failures fail closed. The adapter may distinguish an
  invalid session from an infrastructure error for logging and service-specific
  response policy.
- `platform_admin`, `super_admin`, management permission names, and tenant status
  rules remain outside the generic security package.

## Package structure

The new package is organized below `devops-infra/security`:

```text
security/
├── jwt/           # Claims, token configuration, issue and parse
├── session/       # SessionStore contract and Redis implementation
├── authz/         # Casbin authorizer, model, policy operations
└── middleware/    # framework-neutral decisions and Gin adapters
```

The exact file split may be adjusted during implementation if public boundaries are
unchanged.

### JWT

`security/jwt` owns the token contract and validation:

- access-token signing and parsing;
- issuer and expiration validation;
- an explicit RS256 algorithm allow-list;
- `kid` header emission and JWKS key selection;
- compatibility with the current claim names: `uid`, `username`, `sid`, `tid`,
  and `tcode`;
- a `TokenManager` interface for service use.

The shared claims type keeps `sid` as a string because the current refresh token is a
UUID session ID. Tenant fields are present for compatibility but are not interpreted
by the generic JWT parser. The private signing key is never exposed through the
verification API.

### Session

`security/session` owns the session contract and the existing Redis implementation.
It preserves these key shapes:

```text
t{tenant_id}:auth:refresh:{sid}
t{tenant_id}:auth:sids:{user_id}
auth:sid2uid:{sid}
```

The contract covers create, active-check, revoke-current-session, revoke-all-user
sessions, and revoke-all-tenant sessions. Refresh rotation creates the new session
before removing the old one, matching the current behavior.

The implementation depends on the existing `devops-infra/cache.Cache` abstraction
where possible. Direct Redis operations remain internal to the implementation for
set membership, scan, and pipeline operations.

### Authorization

`security/authz` owns a configured, instance-based Casbin authorizer. It must not
use a package-global enforcer. Initialization accepts a GORM database and creates
the existing GORM adapter and tenant-domain model.

The model continues to support:

- `g(user, role, tenant)` tenant role membership;
- `g2(user, platform-role)` platform role membership;
- `p(role, domain, permission)` policies;
- wildcard domain `*`;
- `keyMatch` permission matching.

The public authorizer contract includes:

```go
type Authorizer interface {
    Enforce(ctx context.Context, subject, domain, permission string) (bool, error)
    EnforcePlatform(ctx context.Context, subject, permission string) (bool, error)
    IsPlatformAdmin(ctx context.Context, subject string) (bool, error)
}
```

Policy maintenance methods for rebuilding/removing role policies and user grouping
policies are exposed through an implementation interface used by `devops-system`.
The generic package does not define management-plane role names or permission
constants. All authorization failures are returned as errors or `false` results;
callers decide how to expose them.

### Middleware

`security/middleware` provides:

- a framework-neutral authentication operation that reads a bearer token, parses
  claims, and optionally checks the session store;
- framework-neutral permission decisions for a tenant domain or platform domain;
- Gin adapters that set claims under a documented context key and delegate 401/403
  output to injected handlers.

The current `devops-system` adapters preserve its response messages and HTTP status
codes. The `Tenant` middleware remains in `devops-system`, because tenant status,
expiry, and tenant database context are business concerns.

## Integration and migration

1. Add the shared security packages and focused unit tests to `devops-infra`.
2. Make `devops-system` configuration map its existing JWT values into the shared
   configuration types.
3. Replace `internal/auth.Manager` internals with the shared token/session
   implementation, retaining a compatibility facade where existing service
   signatures make that useful.
4. Replace the global `internal/authz` enforcer with an injected shared authorizer.
   Existing seed and policy operations remain available through a system-level
   facade until all callers use the new interface.
5. Update JWT and Casbin middleware to use the shared Gin adapters.
6. Remove duplicate implementation only after all callers and tests use the shared
   package.

No route path, response envelope, login/refresh/logout behavior, Redis key, or
Casbin policy row shape changes as part of this extraction.

## Error handling

- Invalid/missing bearer tokens, invalid signatures, wrong issuers, expired tokens,
  and inactive sessions are authentication failures.
- Casbin evaluation errors fail closed and are logged with subject, domain, and
  permission context.
- Redis/session infrastructure errors are not converted into successful auth. The
  Gin adapter maps them according to the service's injected error writer; the first
  consumer preserves the current unauthorized behavior while logging the underlying
  error.
- No broad catch or success-shaped fallback is introduced.

## Testing

### Shared package tests

- JWT signing, parsing, expiration, issuer, and algorithm rejection;
- claims compatibility with current `uid/username/sid/tid/tcode` tokens;
- session create, active check, rotation, logout, user revoke, and tenant revoke;
- Casbin tenant isolation, platform-admin wildcard access, tenant-super-admin
  isolation, and policy rebuild/remove operations;
- middleware 401, 403, success, claims injection, and fail-closed behavior.

### Consumer regression tests

- existing `devops-system` login, refresh, logout, password-change revocation,
  tenant-disable revocation, and route authorization behavior;
- compatibility against existing Redis sessions and `casbin_rule` rows;
- router wiring to ensure permission middleware executes before handlers.

Validation uses the smallest relevant Go test commands first, followed by package
build/type-check and the existing service test suite when targeted tests pass.

## Non-goals

- creating a separate authentication/authorization microservice;
- changing the management-plane role model;
- moving tenant lifecycle or tenant database routing into `devops-infra`;
- adding OAuth/OIDC or a new token version;
- changing public API response formats.
