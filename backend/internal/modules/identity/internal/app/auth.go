package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/mail"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/observability"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// ResolveStore finds the tenant named on the staff login form. Stores that do not
// exist or are suspended produce the same answer as wrong credentials.
func (s *Service) ResolveStore(ctx context.Context, slug string) (uuid.UUID, error) {
	var tenantID uuid.UUID
	var status string
	err := s.pool.QueryRow(ctx, `SELECT tenant_id, status FROM core.resolve_tenant_by_slug($1)`, slug).Scan(&tenantID, &status)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && status != "active") {
		return uuid.Nil, invalidCredentials()
	}
	if err != nil {
		return uuid.Nil, fmt.Errorf("resolve store: %w", err)
	}
	return tenantID, nil
}

// Register creates a customer account. The answer is the same whether or not the
// e-mail is already in use, and the owner of the address is told by e-mail.
func (s *Service) Register(ctx context.Context, portal Portal, rawName, rawEmail, password, linkBase string) error {
	if !portal.SelfRegistration {
		return apperr.New(apperr.KindForbidden, "REGISTRATION_DISABLED", "Accounts here are created by invitation.")
	}
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return err
	}
	name, err := domain.NormalizeName(rawName)
	if err != nil {
		return err
	}
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	if err := domain.ValidatePassword(password, email); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	var token string
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		userID := newID()
		if err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
			TenantID: tenantID, ID: userID, Realm: portal.Realm, Email: email, Name: name,
			PasswordHash: &hash, Status: domain.StatusActive,
		}); err != nil {
			return err
		}
		token, err = createUserToken(ctx, q, tenantID, userID, domain.TokenEmailVerification, s.now().Add(verifyTokenTTL))
		if err != nil {
			return err
		}
		return selfAudit(ctx, tx, tenantID, userID, "identity.user.registered")
	})
	if isUniqueViolation(err) {
		s.mailer.Enqueue(ctx, mail.Message{
			Kind: "identity.duplicate_signup", To: email, Subject: "Tentativa de cadastro na sua conta",
			Body: "Alguém tentou criar uma conta com este e-mail, que já está cadastrado.\n\n" +
				"Se foi você, entre na loja ou recupere sua senha em:\n" + linkBase + "/recuperar-senha\n\n" +
				"Se não foi você, ignore esta mensagem.",
		})
		return nil
	}
	if err != nil {
		return fmt.Errorf("register user: %w", err)
	}
	s.mailer.Enqueue(ctx, mail.Message{
		Kind: "identity.email_verification", To: email, Subject: "Confirme seu e-mail",
		Body: "Confirme seu e-mail acessando o endereço abaixo. Ele vale por 48 horas.\n\n" +
			linkBase + "/verificar-email?token=" + token,
	})
	return nil
}

// Login verifies a password and starts a session or asks for the second factor.
func (s *Service) Login(ctx context.Context, portal Portal, rawEmail, password string, client Client) (AuthResult, error) {
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return AuthResult{}, err
	}
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		s.hasher.VerifyDummy(ctx, password)
		return AuthResult{}, invalidCredentials()
	}

	var user sqlcgen.IdentityUser
	found := true
	err = s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		var err error
		user, err = q.GetUserByEmail(ctx, sqlcgen.GetUserByEmailParams{TenantID: tenantID, Realm: portal.Realm, Email: email})
		if errors.Is(err, pgx.ErrNoRows) {
			found = false
			return nil
		}
		return err
	})
	if err != nil {
		return AuthResult{}, fmt.Errorf("load user: %w", err)
	}
	if !found || user.PasswordHash == nil || user.Status != domain.StatusActive {
		s.hasher.VerifyDummy(ctx, password)
		var userID *uuid.UUID
		if found {
			userID = &user.ID
		}
		s.recordEvent(ctx, portal, userID, "login_failed", client)
		return AuthResult{}, invalidCredentials()
	}

	valid, err := s.hasher.Verify(ctx, password, *user.PasswordHash)
	if err != nil {
		return AuthResult{}, fmt.Errorf("verify password: %w", err)
	}
	if !valid {
		s.recordEvent(ctx, portal, &user.ID, "login_failed", client)
		return AuthResult{}, invalidCredentials()
	}
	s.upgradeHash(ctx, user, password)

	switch {
	case user.MfaEnabledAt != nil:
		return s.newChallenge(ctx, user, StatusMFARequired)
	case portal.MFARequired:
		return s.newChallenge(ctx, user, StatusMFAEnrollmentRequired)
	}

	var result AuthResult
	err = s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		var err error
		result, err = s.startSession(ctx, q, portal, user, client)
		return err
	})
	if err != nil {
		return AuthResult{}, err
	}
	return result, nil
}

func (s *Service) upgradeHash(ctx context.Context, user sqlcgen.IdentityUser, password string) {
	if !s.hasher.NeedsRehash(*user.PasswordHash) {
		return
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err == nil {
		err = s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
			return q.UpdatePassword(ctx, sqlcgen.UpdatePasswordParams{TenantID: user.TenantID, ID: user.ID, PasswordHash: &hash})
		})
	}
	if err != nil {
		s.logger.WarnContext(ctx, "identity.rehash_failed", "error", err)
	}
}

// startSession creates a session in the current transaction.
func (s *Service) startSession(ctx context.Context, q *sqlcgen.Queries, portal Portal, user sqlcgen.IdentityUser, client Client) (AuthResult, error) {
	sessionID := newID()
	err := q.CreateSession(ctx, sqlcgen.CreateSessionParams{
		TenantID: user.TenantID, ID: sessionID, UserID: user.ID, DeviceLabel: client.Device,
		Ip: client.IP, ExpiresAt: s.now().Add(sessionTTL),
	})
	if err != nil {
		return AuthResult{}, fmt.Errorf("create session: %w", err)
	}
	if err := q.InsertAccessEvent(ctx, sqlcgen.InsertAccessEventParams{
		TenantID: user.TenantID, ID: newID(), UserID: &user.ID, Realm: portal.Realm, Event: "login_succeeded",
		Device: client.Device, Ip: client.IP,
	}); err != nil {
		return AuthResult{}, fmt.Errorf("record login: %w", err)
	}
	return s.issueTokens(ctx, q, portal, user, sessionID)
}

func (s *Service) issueTokens(ctx context.Context, q *sqlcgen.Queries, portal Portal, user sqlcgen.IdentityUser, sessionID uuid.UUID) (AuthResult, error) {
	secret, err := security.NewToken()
	if err != nil {
		return AuthResult{}, err
	}
	err = q.InsertRefreshToken(ctx, sqlcgen.InsertRefreshTokenParams{
		TenantID: user.TenantID, ID: newID(), SessionID: sessionID, TokenHash: security.HashToken(secret),
	})
	if err != nil {
		return AuthResult{}, fmt.Errorf("insert refresh token: %w", err)
	}
	access, err := s.issuer.Issue(portal.Audience, user.TenantID, user.ID, sessionID, user.Realm)
	if err != nil {
		return AuthResult{}, err
	}
	view, err := s.userView(ctx, q, user)
	if err != nil {
		return AuthResult{}, err
	}
	return AuthResult{
		Status: StatusAuthenticated, AccessToken: access, ExpiresIn: int(accessTokenTTL.Seconds()),
		User: view, RefreshToken: composeToken(user.TenantID, secret),
	}, nil
}

// Refresh rotates a refresh token. Using an already rotated token means it may
// have been stolen, so the whole session is revoked.
func (s *Service) Refresh(ctx context.Context, portal Portal, token string, client Client) (AuthResult, error) {
	ctx, secret, err := bindToken(ctx, portal, token)
	if err != nil {
		return AuthResult{}, sessionExpired()
	}
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return AuthResult{}, err
	}

	var result AuthResult
	var outcome error
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		row, err := q.LockRefreshToken(ctx, sqlcgen.LockRefreshTokenParams{TenantID: tenantID, TokenHash: security.HashToken(secret)})
		if errors.Is(err, pgx.ErrNoRows) {
			outcome = sessionExpired()
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock refresh token: %w", err)
		}
		if row.RevokedAt != nil || !s.now().Before(row.ExpiresAt) {
			outcome = sessionExpired()
			return nil
		}
		if row.UsedAt != nil {
			if s.now().Sub(*row.UsedAt) < refreshReuseGrace {
				outcome = apperr.New(apperr.KindConflict, "REFRESH_IN_PROGRESS", "The session was just renewed. Try again.")
				return nil
			}
			reason := "refresh_token_reuse"
			if _, err := q.RevokeSession(ctx, sqlcgen.RevokeSessionParams{
				TenantID: tenantID, UserID: row.UserID, ID: row.SessionID, RevokedReason: &reason,
			}); err != nil {
				return fmt.Errorf("revoke session: %w", err)
			}
			s.logger.WarnContext(ctx, "security.refresh_token_reuse", "session_id", row.SessionID)
			observability.SecurityEvent(ctx, "refresh_token_reuse")
			outcome = sessionExpired()
			return selfAudit(ctx, tx, tenantID, row.UserID, "identity.session.reuse_detected")
		}

		user, err := q.GetUserByID(ctx, sqlcgen.GetUserByIDParams{TenantID: tenantID, ID: row.UserID})
		if err != nil {
			return fmt.Errorf("load user: %w", err)
		}
		if user.Status != domain.StatusActive || user.Realm != portal.Realm {
			outcome = sessionExpired()
			return nil
		}
		if err := q.MarkRefreshTokenUsed(ctx, sqlcgen.MarkRefreshTokenUsedParams{TenantID: tenantID, ID: row.ID}); err != nil {
			return fmt.Errorf("mark refresh token: %w", err)
		}
		if err := q.TouchSession(ctx, sqlcgen.TouchSessionParams{TenantID: tenantID, ID: row.SessionID, Ip: client.IP}); err != nil {
			return fmt.Errorf("touch session: %w", err)
		}
		result, err = s.issueTokens(ctx, q, portal, user, row.SessionID)
		return err
	})
	if err != nil {
		return AuthResult{}, err
	}
	if outcome != nil {
		return AuthResult{}, outcome
	}
	return result, nil
}

// Logout ends the session of a refresh token. Unknown tokens are ignored.
func (s *Service) Logout(ctx context.Context, portal Portal, token string) error {
	ctx, secret, err := bindToken(ctx, portal, token)
	if err != nil {
		return nil //nolint:nilerr // There is no session to end.
	}
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return err
	}
	return s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		row, err := q.LockRefreshToken(ctx, sqlcgen.LockRefreshTokenParams{TenantID: tenantID, TokenHash: security.HashToken(secret)})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock refresh token: %w", err)
		}
		reason := "logout"
		_, err = q.RevokeSession(ctx, sqlcgen.RevokeSessionParams{
			TenantID: tenantID, UserID: row.UserID, ID: row.SessionID, RevokedReason: &reason,
		})
		return err
	})
}

// Authenticate validates an access token and confirms that its session, account
// and store are still active, so a revocation takes effect immediately.
func (s *Service) Authenticate(ctx context.Context, portal Portal, raw string) (authz.Principal, error) {
	claims, err := s.issuer.Verify(raw, portal.Audience)
	if err != nil || claims.Realm != portal.Realm {
		return authz.Principal{}, sessionExpired()
	}
	userID, err := uuid.Parse(claims.Subject)
	if err != nil {
		return authz.Principal{}, sessionExpired()
	}
	if current, bound := tenancy.FromContext(ctx); bound && current != claims.TenantID {
		s.logger.WarnContext(ctx, "security.tenant_mismatch", "portal", portal.Name)
		observability.SecurityEvent(ctx, "tenant_mismatch")
		return authz.Principal{}, sessionExpired()
	}

	ctx = tenancy.WithTenant(ctx, claims.TenantID)
	principal := authz.Principal{UserID: userID, TenantID: claims.TenantID, SessionID: claims.SessionID, Realm: claims.Realm}
	var active bool
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		session, err := q.GetActiveSession(ctx, sqlcgen.GetActiveSessionParams{TenantID: claims.TenantID, ID: claims.SessionID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("load session: %w", err)
		}
		user, err := q.GetUserByID(ctx, sqlcgen.GetUserByIDParams{TenantID: claims.TenantID, ID: userID})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("load user: %w", err)
		}
		if session.UserID != userID || user.Status != domain.StatusActive || user.Realm != portal.Realm {
			return nil
		}
		if err := tx.QueryRow(ctx, `SELECT core.tenant_is_active($1)`, claims.TenantID).Scan(&active); err != nil {
			return fmt.Errorf("check tenant: %w", err)
		}
		principal.Permissions, err = q.ListUserPermissions(ctx, sqlcgen.ListUserPermissionsParams{TenantID: claims.TenantID, UserID: userID})
		return err
	})
	if err != nil {
		return authz.Principal{}, err
	}
	if !active {
		return authz.Principal{}, sessionExpired()
	}
	return principal, nil
}

// ForgotPassword sends a reset link when the account exists. The caller always
// receives the same answer.
func (s *Service) ForgotPassword(ctx context.Context, portal Portal, rawEmail, linkBase string) error {
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return err
	}
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		return nil //nolint:nilerr // The answer must not reveal anything about the input.
	}

	var token string
	err = s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		user, err := q.GetUserByEmail(ctx, sqlcgen.GetUserByEmailParams{TenantID: tenantID, Realm: portal.Realm, Email: email})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("load user: %w", err)
		}
		if user.Status != domain.StatusActive {
			return nil
		}
		token, err = createUserToken(ctx, q, tenantID, user.ID, domain.TokenPasswordReset, s.now().Add(resetTokenTTL))
		return err
	})
	if err != nil {
		return err
	}
	if token != "" {
		s.mailer.Enqueue(ctx, mail.Message{
			Kind: "identity.password_reset", To: email, Subject: "Redefinição de senha",
			Body: "Para criar uma nova senha, acesse o endereço abaixo. Ele vale por 1 hora.\n\n" +
				linkBase + "/redefinir-senha?token=" + token + "\n\nSe você não pediu a redefinição, ignore esta mensagem.",
		})
	}
	return nil
}

// ResetPassword replaces the password and ends every session of the account.
func (s *Service) ResetPassword(ctx context.Context, portal Portal, token, password string) error {
	ctx, secret, err := bindToken(ctx, portal, token)
	if err != nil {
		return err
	}
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return err
	}
	if err := domain.ValidatePassword(password, ""); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	return s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		user, err := consumeToken(ctx, q, tenantID, secret, domain.TokenPasswordReset, portal)
		if err != nil {
			return err
		}
		if err := domain.ValidatePassword(password, user.Email); err != nil {
			return err
		}
		if err := q.UpdatePassword(ctx, sqlcgen.UpdatePasswordParams{TenantID: tenantID, ID: user.ID, PasswordHash: &hash}); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		reason := "password_reset"
		if _, err := q.RevokeAllSessions(ctx, sqlcgen.RevokeAllSessionsParams{TenantID: tenantID, UserID: user.ID, RevokedReason: &reason}); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		return selfAudit(ctx, tx, tenantID, user.ID, "identity.password.reset")
	})
}

func consumeToken(ctx context.Context, q *sqlcgen.Queries, tenantID uuid.UUID, secret, kind string, portal Portal) (sqlcgen.IdentityUser, error) {
	userID, err := q.ConsumeUserToken(ctx, sqlcgen.ConsumeUserTokenParams{TenantID: tenantID, TokenHash: security.HashToken(secret), Kind: kind})
	if errors.Is(err, pgx.ErrNoRows) {
		return sqlcgen.IdentityUser{}, invalidToken()
	}
	if err != nil {
		return sqlcgen.IdentityUser{}, fmt.Errorf("consume token: %w", err)
	}
	user, err := q.GetUserByID(ctx, sqlcgen.GetUserByIDParams{TenantID: tenantID, ID: userID})
	if err != nil {
		return sqlcgen.IdentityUser{}, fmt.Errorf("load user: %w", err)
	}
	if user.Realm != portal.Realm || user.Status == domain.StatusDisabled {
		return sqlcgen.IdentityUser{}, invalidToken()
	}
	return user, nil
}

// VerifyEmail confirms the address of an account.
func (s *Service) VerifyEmail(ctx context.Context, portal Portal, token string) error {
	ctx, secret, err := bindToken(ctx, portal, token)
	if err != nil {
		return err
	}
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return err
	}
	return s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		user, err := consumeToken(ctx, q, tenantID, secret, domain.TokenEmailVerification, portal)
		if err != nil {
			return err
		}
		return q.MarkEmailVerified(ctx, sqlcgen.MarkEmailVerifiedParams{TenantID: tenantID, ID: user.ID})
	})
}

// AcceptInvitation activates an invited account with the chosen password.
func (s *Service) AcceptInvitation(ctx context.Context, portal Portal, token, rawName, password string) error {
	ctx, secret, err := bindToken(ctx, portal, token)
	if err != nil {
		return err
	}
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return err
	}
	name, err := domain.NormalizeName(rawName)
	if err != nil {
		return err
	}
	if err := domain.ValidatePassword(password, ""); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(ctx, password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}

	return s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		user, err := consumeToken(ctx, q, tenantID, secret, domain.TokenInvitation, portal)
		if err != nil {
			return err
		}
		if err := domain.ValidatePassword(password, user.Email); err != nil {
			return err
		}
		affected, err := q.ActivateInvitedUser(ctx, sqlcgen.ActivateInvitedUserParams{
			TenantID: tenantID, ID: user.ID, Name: name, PasswordHash: &hash,
		})
		if err != nil {
			return fmt.Errorf("activate user: %w", err)
		}
		if affected == 0 {
			return invalidToken()
		}
		return selfAudit(ctx, tx, tenantID, user.ID, "identity.invitation.accepted")
	})
}

// WithTenant exposes tenant binding to the HTTP adapter.
func WithTenant(ctx context.Context, tenantID uuid.UUID) context.Context {
	return tenancy.WithTenant(ctx, tenantID)
}
