# Shared Authentication and Authorization Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extract the RS256/JWKS JWT, compatible session, and tenant-domain Casbin capabilities into `devops-infra/security`, then migrate `devops-system` to use the shared implementations without changing its public behavior.

**Architecture:** Build framework-independent RS256 JWT, Redis session, and Casbin authorizer packages in `devops-infra/security`, with Gin adapters that accept service-specific error writers. Higress and each backend verify Access Tokens using JWKS-selected public keys; backends additionally check Redis Session state. Keep `devops-system` ownership of tenant lifecycle, management roles, permission constants, policy seeds, and compatibility facades while replacing its duplicated security internals with injected shared instances.

**Tech Stack:** Go 1.26, `github.com/golang-jwt/jwt/v5`, `github.com/redis/go-redis/v9`, `github.com/casbin/casbin/v3`, `github.com/casbin/gorm-adapter/v3`, GORM, Gin, Redis, MySQL.

**Spec:** [docs/superpowers/specs/2026-09-18-shared-authz-security-design.md](../specs/2026-09-18-shared-authz-security-design.md)

## Global Constraints

- Existing refresh session IDs, Redis keys, and Casbin rows remain compatible. Existing HS256 access tokens are invalidated at rollout; refresh sessions remain usable to obtain RS256 tokens.
- Access Tokens use RS256 and include `kid`; private keys are held by configuration/secrets management and public keys are exposed to Higress/services through JWKS.
- The initial implementation is a shared Go package, not a standalone authentication or authorization service.
- Core security decisions are framework-independent. Gin integration is an adapter.
- Public APIs return Go errors and typed results; the infrastructure package does not depend on `devops-system` response formatting.
- Authentication and session failures fail closed.
- `platform_admin`, `super_admin`, management permission names, and tenant status rules remain outside the generic security package.
- No route path, response envelope, login/refresh/logout behavior, Redis key, or Casbin policy row shape changes.

---

## File Map

### New shared infrastructure files

- `devops-infra/security/jwt/config.go`: RS256/JWKS configuration, subject, token pair, and claims types.
- `devops-infra/security/jwt/manager.go`: access-token issue and parse implementation.
- `devops-infra/security/jwt/manager_test.go`: JWT compatibility and validation tests.
- `devops-infra/security/session/session.go`: session references, session store contract, and errors.
- `devops-infra/security/session/redis.go`: Redis-backed implementation and current key protocol.
- `devops-infra/security/session/redis_test.go`: session lifecycle tests using `github.com/alicebob/miniredis/v2`.
- `devops-infra/security/authz/model.go`: tenant-domain Casbin model constant and model construction.
- `devops-infra/security/authz/authorizer.go`: instance-based Casbin authorizer and enforcement API.
- `devops-infra/security/authz/policy.go`: generic policy/grouping maintenance operations.
- `devops-infra/security/authz/authorizer_test.go`: tenant/platform isolation and policy tests.
- `devops-infra/security/middleware/authenticate.go`: framework-neutral bearer authentication decision.
- `devops-infra/security/middleware/gin.go`: Gin context claims storage and injectable HTTP error handlers.
- `devops-infra/security/middleware/gin_test.go`: adapter status and context tests.

### Modified shared infrastructure files

- `devops-infra/go.mod`: direct Casbin, Casbin GORM adapter, JWT, `github.com/alicebob/miniredis/v2` for Redis tests, and `gorm.io/driver/sqlite` for Casbin tests.

### `devops-system` compatibility and migration files

- `devops-system/internal/config/config.go`: map existing JWT configuration to shared JWT configuration without changing YAML keys.
- `devops-system/internal/auth/auth.go`: become a compatibility facade over shared JWT and session managers, retaining `AccessClaims`, `Issue`, `Parse`, `SessionActive`, `LookupSID`, `SIDMatch`, `Rotate`, `Logout`, and revoke methods.
- `devops-system/internal/authz/authz.go`: become an injected compatibility facade over the shared authorizer; retain management-specific seed functions and current call signatures.
- `devops-system/internal/middleware/jwt.go`: use the shared Gin adapter while retaining `CurrentClaims` and current response messages.
- `devops-system/internal/middleware/casbin.go`: use the shared authorizer adapter while retaining `Perm` and `PlatformPerm`.
- `devops-system/internal/cmd/root.go`: construct shared JWT/session/authorizer instances and inject them into compatibility facades.
- `devops-system/internal/cmd/migrate.go`: initialize and close an authorizer instance instead of relying on a package-global enforcer.
- `devops-system/go.mod`: replace direct JWT/Casbin implementation dependencies with the shared module dependencies only when no remaining system code imports them.

### Tests and documentation

- Add focused tests beside each new shared package.
- Extend existing `devops-system` tests or add package tests for facade compatibility and router middleware ordering.
- Update the relevant package comments and module documentation if the repository has an existing security integration document; do not add unrelated documentation.

---

## Task 1: Add shared JWT contracts and implementation

**Files:**
- Create: `devops-infra/security/jwt/config.go`
- Create: `devops-infra/security/jwt/manager.go`
- Create: `devops-infra/security/jwt/manager_test.go`
- Modify: `devops-infra/go.mod`
- Modify: `devops-infra/go.sum`

**Interfaces:**
- Produces `jwt.Config`, `jwt.Claims`, `jwt.Subject`, `jwt.TokenPair`, and `jwt.Manager`.
- `Manager.Issue(ctx context.Context, subject Subject, sessionID string) (TokenPair, error)`.
- `Manager.ParseAccess(token string) (*Claims, error)`.
- `Config` contains an RSA private-key PEM for issuing, a `KeyID` for the `kid`
  header, and a public-key set keyed by `kid` for verification. Verification
  accepts a refreshable `KeySet` interface so services can back it with JWKS.
- Claims use JSON names `uid`, `username`, `sid`, `tid`, and `tcode`, with `sid` as `string`.

- [ ] **Step 1: Add the shared JWT dependency and define the public types.**

Define configuration fields `PrivateKeyPEM []byte`, `KeyID string`, `Issuer string`, `AccessExpire time.Duration`, and `RefreshExpire time.Duration`. Define `Subject` with `UserID`, `Username`, `TenantID`, and `TenantCode`; define `Claims` with those fields, `SessionID string`, and `jwt.RegisteredClaims`; define `TokenPair` with access token, session ID, and access expiry seconds. Define a `KeySet` lookup interface for public RSA keys by `kid`.

- [ ] **Step 2: Write failing tests for current-token compatibility and validation.**

Cover: an issued token uses RS256, contains `kid` and the five current claim names; a valid token parses through a key set; an expired token fails; a wrong issuer fails; a missing/unknown `kid` fails; a non-RS256 algorithm fails; and malformed private/public key configuration returns an error instead of issuing or verifying a token.

- [ ] **Step 3: Implement issue and parse with explicit RS256/JWKS validation.**

Parse the configured RSA private key once during manager construction and sign with
`jwt.SigningMethodRS256`, setting the `kid` header. Parse public keys from the
configured `KeySet` by header `kid`; reject any method that is not exactly
`jwt.SigningMethodRS256`; use `jwt.WithIssuer`; and return an unauthorized typed
error without exposing key material. Do not make the verification path depend on the
private key.

- [ ] **Step 4: Run the focused JWT tests.**

Run `go test ./security/jwt` from `devops-infra`.

Expected: all JWT tests pass, including RS256, `kid`, public-key lookup, and rejection tests.

- [ ] **Step 5: Commit the shared JWT package.**

Run:

```text
git add devops-infra/security/jwt devops-infra/go.mod devops-infra/go.sum
git commit -m "feat: add shared jwt manager"
```

## Task 2: Add the shared Redis session contract and implementation

**Files:**
- Create: `devops-infra/security/session/session.go`
- Create: `devops-infra/security/session/redis.go`
- Create: `devops-infra/security/session/redis_test.go`
- Modify: `devops-infra/go.mod`
- Modify: `devops-infra/go.sum`

**Interfaces:**
- Produces `session.Session`, `session.SessionRef`, `session.Store`, and `session.RedisStore`.
- `Store.Create(ctx context.Context, s Session) error`.
- `Store.Active(ctx context.Context, ref SessionRef) (bool, error)`.
- `Store.Lookup(ctx context.Context, sessionID string) (tenantID, userID int64, ok bool)`.
- `Store.Matches(ctx context.Context, ref SessionRef, userID int64) (bool, error)`.
- `Store.Revoke(ctx context.Context, ref SessionRef) error`.
- `Store.RevokeUser(ctx context.Context, tenantID, userID int64) error`.
- `Store.RevokeTenant(ctx context.Context, tenantID int64) error`.

- [ ] **Step 1: Define session types, errors, and key helpers.**

Keep the exact key formats `t{tenant_id}:auth:refresh:{sid}`, `t{tenant_id}:auth:sids:{user_id}`, and `auth:sid2uid:{sid}`. Validate that tenant/user IDs are positive and session IDs are non-empty before writing keys; return explicit errors for invalid input.

- [ ] **Step 2: Write lifecycle tests against the existing Redis client abstraction.**

Test create plus active, lookup, user matching, revoke, revoke-user, and revoke-tenant. Test that lookup rejects malformed `tenant:user` index values. Test that the set and index are removed during revoke and rotation support can safely create a new session before revoking the old one.

- [ ] **Step 3: Implement RedisStore using go-redis pipelines and scan.**

Use the existing `devops-infra/cache/redis` client access pattern only through constructor injection of `*redis.Client`; keep direct Redis operations internal. Return Redis errors to callers; do not turn a Redis outage into `Active == false` without an error.

- [ ] **Step 4: Run session tests and the existing cache tests.**

Run `go test ./security/session ./cache/...` from `devops-infra`.

Expected: session lifecycle tests and existing cache tests pass.

- [ ] **Step 5: Commit the shared session package.**

Run:

```text
git add devops-infra/security/session devops-infra/go.mod devops-infra/go.sum
git commit -m "feat: add shared redis session store"
```

## Task 3: Add the instance-based shared Casbin authorizer

**Files:**
- Create: `devops-infra/security/authz/model.go`
- Create: `devops-infra/security/authz/authorizer.go`
- Create: `devops-infra/security/authz/policy.go`
- Create: `devops-infra/security/authz/authorizer_test.go`
- Modify: `devops-infra/go.mod`
- Modify: `devops-infra/go.sum`

**Interfaces:**
- Produces `authz.Authorizer` and `authz.PolicyManager`.
- `New(gdb *gorm.DB) (*Authorizer, error)`.
- `Authorizer.Enforce(ctx context.Context, subject, domain, permission string) (bool, error)`.
- `Authorizer.EnforcePlatform(ctx context.Context, subject, permission string) (bool, error)`.
- `Authorizer.IsPlatformAdmin(ctx context.Context, subject string) (bool, error)`.
- `PolicyManager.RebuildRolePolicies(role, domain string, permissions []string) error`.
- `PolicyManager.RemoveRolePolicies(role, domain string) error`.
- `PolicyManager.RebuildUserGrouping(subject, domain string, roles []string) error`.
- `PolicyManager.RemoveUserGrouping(subject, domain string) error`.
- `PolicyManager.RemoveRoleGrouping(role, domain string) error`.
- `PolicyManager.AddPolicyIfAbsent(policyType string, values ...string) error`.
- Policy manager methods accept string role/domain/permission values and do not define management role constants.

- [ ] **Step 1: Move the current model into a shared model constructor without changing semantics.**

Preserve `g`, `g2`, `p`, domain wildcard `*`, and `keyMatch`. Build `gorm-adapter/v3` from the supplied DB and call `LoadPolicy` during `New`.

- [ ] **Step 2: Write tests for domain and platform isolation.**

Use `gorm.io/driver/sqlite` with an in-memory database and seed these exact rules: `p, platform_admin, *, *`; `g2, 10, platform_admin`; `p, super_admin, 1, *`; `g, 20, super_admin, 1`; and a tenant permission policy. Assert platform user 10 can enforce platform permissions, tenant user 20 can enforce only tenant 1 permissions, and tenant user 20 cannot reach platform permissions.

- [ ] **Step 3: Implement instance methods with fail-closed errors.**

Convert numeric subjects/domains at the system facade boundary, not inside the generic interface. On Casbin evaluation error return `false` plus the error; log subject/domain/permission with `slog` in the implementation. Never use a package-global enforcer.

- [ ] **Step 4: Implement policy maintenance operations.**

Provide role policy rebuild/removal, user grouping rebuild/removal, role grouping removal, and seed-rule insertion as generic operations over caller-supplied strings. Preserve `casbin_rule` persistence through the adapter.

- [ ] **Step 5: Run focused authorizer tests.**

Run `go test ./security/authz` from `devops-infra`.

Expected: policy loading, tenant isolation, platform wildcard behavior, and maintenance operations pass.

- [ ] **Step 6: Commit the shared authorizer.**

Run:

```text
git add devops-infra/security/authz devops-infra/go.mod devops-infra/go.sum
git commit -m "feat: add shared casbin authorizer"
```

## Task 4: Add framework-neutral and Gin security adapters

**Files:**
- Create: `devops-infra/security/middleware/authenticate.go`
- Create: `devops-infra/security/middleware/gin.go`
- Create: `devops-infra/security/middleware/gin_test.go`
- Modify: `devops-infra/go.mod`
- Modify: `devops-infra/go.sum`

**Interfaces:**
- Produces `middleware.ClaimsContextKey`, `middleware.Authenticator`, and `middleware.GinOptions`.
- `Authenticator.Authenticate(ctx context.Context, bearerHeader string) (*jwt.Claims, error)`.
- Gin adapter stores claims under a documented string key and accepts `OnUnauthorized`, `OnForbidden`, and `OnInfrastructureError` callbacks.

- [ ] **Step 1: Write tests for bearer parsing and decision outcomes.**

Cover missing header, wrong scheme, empty token, invalid token, inactive session, valid token, authorization denial, platform-admin denial, and success. Assert denied requests call `Abort` and successful requests call `Next`.

- [ ] **Step 2: Implement framework-neutral authentication.**

Trim only the exact `Bearer ` prefix used by the current service, call the shared JWT manager, then call the session store with the parsed tenant/session reference. Return typed authentication or infrastructure errors; never inject claims after a failed session check.

- [ ] **Step 3: Implement Gin adapters with injected output handlers.**

Set claims under a stable key, expose a helper to retrieve them, and preserve the middleware ordering contract. Do not import `devops-infra/http/response` from the security package.

- [ ] **Step 4: Run middleware tests and all shared security tests.**

Run `go test ./security/...` from `devops-infra`.

Expected: all shared JWT, session, authorizer, and middleware tests pass.

- [ ] **Step 5: Commit the adapters.**

Run:

```text
git add devops-infra/security/middleware devops-infra/go.mod devops-infra/go.sum
git commit -m "feat: add shared security middleware adapters"
```

## Task 5: Convert `devops-system/internal/auth` to a shared-security facade

**Files:**
- Modify: `devops-system/internal/config/config.go`
- Modify: `devops-system/internal/auth/auth.go`
- Create or modify: `devops-system/internal/auth/auth_test.go`
- Modify: `devops-system/go.mod`

**Interfaces:**
- Preserve all existing `auth.Manager` methods used by `service.Auth` and `middleware.JWT`.
- Preserve `auth.AccessClaims` field names and JSON tags for callers.
- Add an explicit constructor dependency for shared JWT manager and session store, while keeping `auth.New(cfg.JWT)` as a temporary compatibility constructor only if existing call sites cannot be changed in the same task.

- [ ] **Step 1: Add facade tests around the existing public behavior.**

Test `Issue`, `Parse`, `SessionActive`, `LookupSID`, `SIDMatch`, `Rotate`, `Logout`, `RevokeAllSessions`, and `RevokeTenantSessions` through the current signatures. Assert returned claims and Redis key behavior match the current implementation.

- [ ] **Step 2: Map system configuration into shared JWT configuration.**

Keep `jwt.signing_key`, `jwt.issuer`, `jwt.access_expire`, and `jwt.refresh_expire` unchanged in YAML and `config.Config`; only add conversion code to shared config types.

- [ ] **Step 3: Replace duplicated token/session logic with shared implementations.**

Keep `AccessClaims` as a type alias or conversion-compatible wrapper over shared claims. `Issue` generates a session ID, delegates token signing and session creation, and returns the same values. `Rotate` creates the new session before revoking the old one. Existing service error messages remain unchanged.

- [ ] **Step 4: Run auth facade and service tests.**

Run `go test ./internal/auth ./internal/service` from `devops-system`.

Expected: existing auth behavior passes without changing service response types.

- [ ] **Step 5: Commit the auth migration.**

Run:

```text
git add devops-system/internal/config/config.go devops-system/internal/auth devops-system/go.mod
git commit -m "refactor: use shared jwt and session security"
```

## Task 6: Convert `devops-system/internal/authz` to an injected facade

**Files:**
- Modify: `devops-system/internal/authz/authz.go`
- Create or modify: `devops-system/internal/authz/authz_test.go`
- Modify: `devops-system/internal/cmd/root.go`
- Modify: `devops-system/internal/cmd/migrate.go`

**Interfaces:**
- Preserve current calls from services and middleware: `Init`, `Close`, `Enforce`, `EnforcePlatform`, `IsPlatformAdmin`, all policy rebuild/remove functions, `SeedPolicies`, and `SeedTenantAdmin`.
- Internally route these calls to an injected `*securityauthz.Authorizer`.

- [ ] **Step 1: Add facade tests for existing policy operations.**

Verify that the facade preserves current numeric user/tenant conversion, returns errors from policy mutations, and returns false on enforcement errors. Include tenant isolation and platform-admin checks.

- [ ] **Step 2: Replace package-global enforcer state with an instance holder owned by command assembly.**

Create the shared authorizer after MySQL initialization, pass it to the system facade or construct a facade value, and make `Close` release only facade state. If a temporary package-level compatibility holder is required, make initialization explicit and ensure tests can replace it.

- [ ] **Step 3: Keep management policy seeds in `devops-system`.**

Retain `platform_admin`, `super_admin`, seed IDs, and management permission semantics in the system facade. Delegate generic persistence and enforcement to the shared authorizer.

- [ ] **Step 4: Update server and migrate command lifecycle.**

Initialize the authorizer before services/routes, use the same instance for server and migration paths, and close it during the existing shutdown sequence. Do not change migration order: database migration, Casbin adapter/policy load, application seed, and Casbin seed must remain valid.

- [ ] **Step 5: Run authz and command tests.**

Run `go test ./internal/authz ./internal/cmd` from `devops-system`.

Expected: no package-global enforcer behavior leaks between tests or command invocations.

- [ ] **Step 6: Commit the authz migration.**

Run:

```text
git add devops-system/internal/authz devops-system/internal/cmd
git commit -m "refactor: use shared casbin authorizer"
```

## Task 7: Switch `devops-system` middleware to shared Gin adapters

**Files:**
- Modify: `devops-system/internal/middleware/jwt.go`
- Modify: `devops-system/internal/middleware/casbin.go`
- Modify: `devops-system/internal/middleware/tenant.go` only if claims type conversion requires it.
- Modify: `devops-system/internal/router/router.go` only if constructor injection changes signatures.
- Create or modify: `devops-system/internal/middleware/middleware_test.go`

**Interfaces:**
- Preserve `middleware.CurrentClaims(c) *auth.AccessClaims`.
- Preserve `middleware.JWT(m *auth.Manager) gin.HandlerFunc`.
- Preserve `middleware.Perm(perm string)` and `middleware.PlatformPerm(perm string)`.

- [ ] **Step 1: Add middleware regression tests before changing adapters.**

Assert current 401 text for missing/expired/inactive sessions, current 403 text for permission denial, platform-admin gating, and that `Perm` runs before a handler in a router test.

- [ ] **Step 2: Adapt shared claims to `auth.AccessClaims` at the boundary.**

Keep `KeyClaims` stable for current handlers. The adapter may convert shared claims once when injecting them; do not make service handlers depend on the infrastructure package.

- [ ] **Step 3: Replace duplicated middleware decisions with shared Gin adapters.**

Use injected callbacks that call `response.Fail` with the current status/message pairs. Keep `Tenant()` separate and do not add tenant status checks to shared authentication.

- [ ] **Step 4: Run middleware and router regression tests.**

Run `go test ./internal/middleware ./internal/router` from `devops-system`.

Expected: all current response statuses/messages and middleware ordering remain unchanged.

- [ ] **Step 5: Commit the middleware migration.**

Run:

```text
git add devops-system/internal/middleware devops-system/internal/router
git commit -m "refactor: use shared security middleware"
```

## Task 8: Remove duplicate implementation dependencies and validate end to end

**Files:**
- Modify: `devops-system/go.mod`
- Modify: `devops-system/go.sum`
- Modify: package comments in migrated files as needed.
- Modify: `docs/superpowers/specs/2026-09-18-shared-authz-security-design.md` only if implementation decisions materially differ from the approved design.

- [ ] **Step 1: Search for direct old implementation usage.**

Run:

```text
rg "github.com/golang-jwt|github.com/casbin|gorm-adapter|var e \*casbin|internal/authz|internal/auth" devops-system devops-infra
```

Remove only dependencies and duplicate code with no remaining imports; keep compatibility facades required by service code.

- [ ] **Step 2: Run focused package tests together.**

Run:

```text
go test ./security/... 
go test ./internal/auth ./internal/authz ./internal/middleware ./internal/router ./internal/service
```

Expected: both commands pass.

- [ ] **Step 3: Run module-wide tests and build.**

Run:

```text
go test ./...
go build ./...
```

Run the first command from each module (`devops-infra` and `devops-system`) because they are separate Go modules.

- [ ] **Step 4: Verify migration compatibility.**

Against the existing test database/Redis setup, verify: an old access token still parses; an existing refresh SID still looks up; logout and password-change revocation remove active sessions; tenant disable revokes tenant sessions; existing `casbin_rule` rows enforce the same tenant/platform outcomes.

- [ ] **Step 5: Commit cleanup and validation changes.**

Run:

```text
git add devops-infra devops-system
git commit -m "refactor: complete shared security migration"
```

## Self-review checklist

- JWT claims and Redis keys are explicitly preserved.
- The shared Casbin model preserves both tenant `g` and platform `g2` paths.
- No shared package imports `devops-system` or its response package.
- Tenant status and tenant database routing remain in `devops-system`.
- The plan includes tests for every public shared interface and consumer regression points.
- No task uses placeholder wording; every file and command needed for implementation is named.
- Any final implementation deviation must update the approved spec before code is considered complete.
