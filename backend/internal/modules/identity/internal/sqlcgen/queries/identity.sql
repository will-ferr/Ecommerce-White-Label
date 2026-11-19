-- name: CreateUser :exec
INSERT INTO identity.users (tenant_id, id, realm, email, name, password_hash, status)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetUserByEmail :one
SELECT * FROM identity.users WHERE tenant_id = $1 AND realm = $2 AND email = $3;

-- name: GetUserByID :one
SELECT * FROM identity.users WHERE tenant_id = $1 AND id = $2;

-- name: UpdatePassword :exec
UPDATE identity.users SET password_hash = $3, updated_at = now() WHERE tenant_id = $1 AND id = $2;

-- name: MarkEmailVerified :exec
UPDATE identity.users SET email_verified_at = COALESCE(email_verified_at, now()), updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: ActivateInvitedUser :execrows
UPDATE identity.users
SET name = $3, password_hash = $4, status = 'active', email_verified_at = now(), updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND status = 'invited';

-- name: SetUserStatus :exec
UPDATE identity.users SET status = $3, updated_at = now() WHERE tenant_id = $1 AND id = $2;

-- name: SetPendingMFASecret :exec
UPDATE identity.users
SET mfa_secret_encrypted = $3, mfa_enabled_at = NULL, mfa_last_step = 0, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: EnableMFA :exec
UPDATE identity.users SET mfa_enabled_at = now(), updated_at = now() WHERE tenant_id = $1 AND id = $2;

-- name: DisableMFA :exec
UPDATE identity.users
SET mfa_secret_encrypted = NULL, mfa_enabled_at = NULL, mfa_last_step = 0, updated_at = now()
WHERE tenant_id = $1 AND id = $2;

-- name: AdvanceMFAStep :execrows
UPDATE identity.users SET mfa_last_step = $3
WHERE tenant_id = $1 AND id = $2 AND mfa_last_step < $3;

-- name: InsertRecoveryCode :exec
INSERT INTO identity.recovery_codes (tenant_id, id, user_id, code_hash) VALUES ($1, $2, $3, $4);

-- name: DeleteRecoveryCodes :exec
DELETE FROM identity.recovery_codes WHERE tenant_id = $1 AND user_id = $2;

-- name: UseRecoveryCode :execrows
UPDATE identity.recovery_codes SET used_at = now()
WHERE tenant_id = $1 AND user_id = $2 AND code_hash = $3 AND used_at IS NULL;

-- name: CreateSession :exec
INSERT INTO identity.sessions (tenant_id, id, user_id, device_label, ip, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: GetActiveSession :one
SELECT s.id, s.user_id
FROM identity.sessions s
WHERE s.tenant_id = $1 AND s.id = $2 AND s.revoked_at IS NULL AND s.expires_at > now();

-- name: ListActiveSessions :many
SELECT id, device_label, ip, created_at, last_used_at
FROM identity.sessions
WHERE tenant_id = $1 AND user_id = $2 AND revoked_at IS NULL AND expires_at > now()
ORDER BY last_used_at DESC;

-- name: RevokeSession :execrows
UPDATE identity.sessions SET revoked_at = now(), revoked_reason = $4
WHERE tenant_id = $1 AND user_id = $2 AND id = $3 AND revoked_at IS NULL;

-- name: RevokeOtherSessions :execrows
UPDATE identity.sessions SET revoked_at = now(), revoked_reason = $4
WHERE tenant_id = $1 AND user_id = $2 AND id <> $3 AND revoked_at IS NULL;

-- name: RevokeAllSessions :execrows
UPDATE identity.sessions SET revoked_at = now(), revoked_reason = $3
WHERE tenant_id = $1 AND user_id = $2 AND revoked_at IS NULL;

-- name: TouchSession :exec
UPDATE identity.sessions SET last_used_at = now(), ip = $3 WHERE tenant_id = $1 AND id = $2;

-- name: InsertRefreshToken :exec
INSERT INTO identity.refresh_tokens (tenant_id, id, session_id, token_hash) VALUES ($1, $2, $3, $4);

-- name: LockRefreshToken :one
SELECT r.id, r.session_id, r.used_at, s.user_id, s.revoked_at, s.expires_at
FROM identity.refresh_tokens r
JOIN identity.sessions s ON s.tenant_id = r.tenant_id AND s.id = r.session_id
WHERE r.tenant_id = $1 AND r.token_hash = $2
FOR UPDATE OF r, s;

-- name: MarkRefreshTokenUsed :exec
UPDATE identity.refresh_tokens SET used_at = now() WHERE tenant_id = $1 AND id = $2;

-- name: InsertUserToken :exec
INSERT INTO identity.user_tokens (tenant_id, id, user_id, kind, token_hash, expires_at)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: InvalidateUserTokens :exec
UPDATE identity.user_tokens SET used_at = now()
WHERE tenant_id = $1 AND user_id = $2 AND kind = $3 AND used_at IS NULL;

-- name: ConsumeUserToken :one
UPDATE identity.user_tokens SET used_at = now()
WHERE tenant_id = $1 AND token_hash = $2 AND kind = $3 AND used_at IS NULL AND expires_at > now()
RETURNING user_id;

-- name: InsertChallenge :exec
INSERT INTO identity.mfa_challenges (tenant_id, id, user_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4, $5);

-- name: LockChallenge :one
SELECT id, user_id, attempts
FROM identity.mfa_challenges
WHERE tenant_id = $1 AND token_hash = $2 AND consumed_at IS NULL AND expires_at > now()
FOR UPDATE;

-- name: IncrementChallengeAttempts :exec
UPDATE identity.mfa_challenges SET attempts = attempts + 1 WHERE tenant_id = $1 AND id = $2;

-- name: ConsumeChallenge :exec
UPDATE identity.mfa_challenges SET consumed_at = now() WHERE tenant_id = $1 AND id = $2;

-- name: InsertAccessEvent :exec
INSERT INTO identity.access_events (tenant_id, id, user_id, realm, event, device, ip)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: ListAccessEvents :many
SELECT id, event, device, ip, occurred_at
FROM identity.access_events
WHERE tenant_id = $1 AND user_id = $2
ORDER BY occurred_at DESC
LIMIT $3;

-- name: GetUserRole :one
SELECT role_code FROM identity.user_roles WHERE tenant_id = $1 AND user_id = $2;

-- name: ListUserPermissions :many
SELECT rp.permission_code
FROM identity.user_roles ur
JOIN identity.role_permissions rp ON rp.role_code = ur.role_code
WHERE ur.tenant_id = $1 AND ur.user_id = $2
ORDER BY rp.permission_code;

-- name: SetUserRole :exec
INSERT INTO identity.user_roles (tenant_id, user_id, role_code) VALUES ($1, $2, $3)
ON CONFLICT (tenant_id, user_id) DO UPDATE SET role_code = EXCLUDED.role_code;

-- name: GetRole :one
SELECT code, realm, name, rank FROM identity.roles WHERE code = $1;

-- name: ListRoles :many
SELECT code, name, rank FROM identity.roles WHERE realm = $1 ORDER BY rank DESC;

-- name: ListTeam :many
SELECT u.id, u.email, u.name, u.status, u.mfa_enabled_at, ur.role_code, r.name AS role_name
FROM identity.users u
JOIN identity.user_roles ur ON ur.tenant_id = u.tenant_id AND ur.user_id = u.id
JOIN identity.roles r ON r.code = ur.role_code
WHERE u.tenant_id = $1 AND u.realm = $2
ORDER BY u.name, u.id
LIMIT 200;

-- name: LockActiveTopAdmins :many
SELECT u.id
FROM identity.users u
JOIN identity.user_roles ur ON ur.tenant_id = u.tenant_id AND ur.user_id = u.id
WHERE u.tenant_id = $1 AND u.status = 'active' AND ur.role_code = $2
ORDER BY u.id
FOR UPDATE OF u;

-- name: EraseUser :execrows
-- Removes every personal field of a customer account. The row stays so
-- orders keep a consistent reference.
UPDATE identity.users
SET email = 'erased+' || id::text || '@invalid', name = 'Conta excluída', password_hash = NULL,
    email_verified_at = NULL, mfa_secret_encrypted = NULL, mfa_enabled_at = NULL, status = 'erased', updated_at = now()
WHERE tenant_id = $1 AND id = $2 AND realm = 'customer' AND status <> 'erased';

-- name: InvalidateAllUserTokens :exec
UPDATE identity.user_tokens SET used_at = now()
WHERE tenant_id = $1 AND user_id = $2 AND used_at IS NULL;

-- name: ListCustomers :many
SELECT id, email, name, status, email_verified_at, created_at
FROM identity.users
WHERE tenant_id = $1 AND realm = 'customer' AND status <> 'erased'
  AND (sqlc.narg('search')::text IS NULL
       OR email ILIKE '%' || sqlc.narg('search') || '%' OR name ILIKE '%' || sqlc.narg('search') || '%')
  AND (sqlc.narg('after_at')::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg('after_at')::timestamptz, sqlc.narg('after_id')::uuid))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg('row_limit');
