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
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/security"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

const mfaIssuer = "Plataforma"

// MFAEnrollment is what an authenticator app needs.
type MFAEnrollment struct {
	Secret string `json:"secret"`
	URI    string `json:"otpauthUri"`
}

func secretContext(tenantID, userID uuid.UUID) []byte {
	return []byte(tenantID.String() + ":" + userID.String())
}

func challengeExpired() error {
	return apperr.New(apperr.KindUnauthenticated, "CHALLENGE_EXPIRED", "The verification step has expired. Sign in again.")
}

func (s *Service) newChallenge(ctx context.Context, user sqlcgen.IdentityUser, status string) (AuthResult, error) {
	secret, err := security.NewToken()
	if err != nil {
		return AuthResult{}, err
	}
	err = s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		return q.InsertChallenge(ctx, sqlcgen.InsertChallengeParams{
			TenantID: user.TenantID, ID: newID(), UserID: user.ID, TokenHash: security.HashToken(secret),
			ExpiresAt: s.now().Add(challengeTTL),
		})
	})
	if err != nil {
		return AuthResult{}, fmt.Errorf("create challenge: %w", err)
	}
	return AuthResult{Status: status, Challenge: composeToken(user.TenantID, secret)}, nil
}

func (s *Service) prepareEnrollment(ctx context.Context, q *sqlcgen.Queries, user sqlcgen.IdentityUser) (MFAEnrollment, error) {
	if user.MfaEnabledAt != nil {
		return MFAEnrollment{}, apperr.New(apperr.KindConflict, "MFA_ALREADY_ENABLED", "Two-step verification is already enabled.")
	}
	secret, err := domain.NewTOTPSecret()
	if err != nil {
		return MFAEnrollment{}, err
	}
	sealed, err := s.sealer.Seal(secret, secretContext(user.TenantID, user.ID))
	if err != nil {
		return MFAEnrollment{}, err
	}
	err = q.SetPendingMFASecret(ctx, sqlcgen.SetPendingMFASecretParams{TenantID: user.TenantID, ID: user.ID, MfaSecretEncrypted: sealed})
	if err != nil {
		return MFAEnrollment{}, fmt.Errorf("store mfa secret: %w", err)
	}
	return MFAEnrollment{Secret: domain.EncodeTOTPSecret(secret), URI: domain.TOTPURI(mfaIssuer, user.Email, secret)}, nil
}

// matchCode checks a code against the stored secret and refuses reuse of a code
// that was already accepted in the same time window.
func (s *Service) matchCode(ctx context.Context, q *sqlcgen.Queries, user sqlcgen.IdentityUser, code string) (bool, error) {
	if user.MfaSecretEncrypted == nil {
		return false, nil
	}
	secret, err := s.sealer.Open(user.MfaSecretEncrypted, secretContext(user.TenantID, user.ID))
	if err != nil {
		return false, err
	}
	step, ok := domain.MatchTOTP(secret, code, s.now())
	if !ok {
		return false, nil
	}
	affected, err := q.AdvanceMFAStep(ctx, sqlcgen.AdvanceMFAStepParams{TenantID: user.TenantID, ID: user.ID, MfaLastStep: step})
	if err != nil {
		return false, fmt.Errorf("advance mfa step: %w", err)
	}
	return affected == 1, nil
}

func (s *Service) matchCodeOrRecovery(ctx context.Context, q *sqlcgen.Queries, user sqlcgen.IdentityUser, code string) (bool, error) {
	if len(code) <= 6 {
		return s.matchCode(ctx, q, user, code)
	}
	affected, err := q.UseRecoveryCode(ctx, sqlcgen.UseRecoveryCodeParams{
		TenantID: user.TenantID, UserID: user.ID, CodeHash: security.HashToken(domain.NormalizeRecoveryCode(code)),
	})
	if err != nil {
		return false, fmt.Errorf("use recovery code: %w", err)
	}
	return affected == 1, nil
}

func replaceRecoveryCodes(ctx context.Context, q *sqlcgen.Queries, user sqlcgen.IdentityUser) ([]string, error) {
	if err := q.DeleteRecoveryCodes(ctx, sqlcgen.DeleteRecoveryCodesParams{TenantID: user.TenantID, UserID: user.ID}); err != nil {
		return nil, fmt.Errorf("delete recovery codes: %w", err)
	}
	codes, err := domain.NewRecoveryCodes()
	if err != nil {
		return nil, err
	}
	for _, code := range codes {
		err := q.InsertRecoveryCode(ctx, sqlcgen.InsertRecoveryCodeParams{
			TenantID: user.TenantID, ID: newID(), UserID: user.ID,
			CodeHash: security.HashToken(domain.NormalizeRecoveryCode(code)),
		})
		if err != nil {
			return nil, fmt.Errorf("insert recovery code: %w", err)
		}
	}
	return codes, nil
}

func activateMFA(ctx context.Context, tx pgx.Tx, q *sqlcgen.Queries, user sqlcgen.IdentityUser) ([]string, error) {
	if err := q.EnableMFA(ctx, sqlcgen.EnableMFAParams{TenantID: user.TenantID, ID: user.ID}); err != nil {
		return nil, fmt.Errorf("enable mfa: %w", err)
	}
	codes, err := replaceRecoveryCodes(ctx, q, user)
	if err != nil {
		return nil, err
	}
	return codes, selfAudit(ctx, tx, user.TenantID, user.ID, "identity.mfa.enabled")
}

// withChallenge runs fn with a locked challenge. A wrong code is committed as a
// failed attempt, and the challenge is discarded after too many of them.
func (s *Service) withChallenge(ctx context.Context, portal Portal, challenge string,
	fn func(tx pgx.Tx, q *sqlcgen.Queries, user sqlcgen.IdentityUser) (bool, error),
) error {
	ctx, secret, err := bindToken(ctx, portal, challenge)
	if err != nil {
		return challengeExpired()
	}
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return err
	}

	var outcome error
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		row, err := q.LockChallenge(ctx, sqlcgen.LockChallengeParams{TenantID: tenantID, TokenHash: security.HashToken(secret)})
		if errors.Is(err, pgx.ErrNoRows) {
			outcome = challengeExpired()
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock challenge: %w", err)
		}
		user, err := q.GetUserByID(ctx, sqlcgen.GetUserByIDParams{TenantID: tenantID, ID: row.UserID})
		if err != nil {
			return fmt.Errorf("load user: %w", err)
		}
		if user.Realm != portal.Realm || user.Status != domain.StatusActive {
			outcome = challengeExpired()
			return nil
		}

		accepted, err := fn(tx, q, user)
		if err != nil {
			return err
		}
		if accepted {
			return q.ConsumeChallenge(ctx, sqlcgen.ConsumeChallengeParams{TenantID: tenantID, ID: row.ID})
		}

		outcome = invalidCode()
		if row.Attempts+1 >= maxMFAAttempts {
			outcome = challengeExpired()
			return q.ConsumeChallenge(ctx, sqlcgen.ConsumeChallengeParams{TenantID: tenantID, ID: row.ID})
		}
		return q.IncrementChallengeAttempts(ctx, sqlcgen.IncrementChallengeAttemptsParams{TenantID: tenantID, ID: row.ID})
	})
	if err != nil {
		return err
	}
	return outcome
}

// CompleteChallenge finishes a login with an authenticator or recovery code.
func (s *Service) CompleteChallenge(ctx context.Context, portal Portal, challenge, code string, client Client) (AuthResult, error) {
	var result AuthResult
	err := s.withChallenge(ctx, portal, challenge, func(_ pgx.Tx, q *sqlcgen.Queries, user sqlcgen.IdentityUser) (bool, error) {
		if user.MfaEnabledAt == nil {
			return false, nil
		}
		accepted, err := s.matchCodeOrRecovery(ctx, q, user, code)
		if err != nil || !accepted {
			return false, err
		}
		result, err = s.startSession(ctx, q, portal, user, client)
		return err == nil, err
	})
	return result, err
}

// StartRequiredEnrollment prepares the authenticator of an account that must use
// two-step verification before its first session.
func (s *Service) StartRequiredEnrollment(ctx context.Context, portal Portal, challenge string) (MFAEnrollment, error) {
	if !portal.MFARequired {
		return MFAEnrollment{}, challengeExpired()
	}
	ctx, secret, err := bindToken(ctx, portal, challenge)
	if err != nil {
		return MFAEnrollment{}, challengeExpired()
	}
	tenantID, err := tenantOf(ctx)
	if err != nil {
		return MFAEnrollment{}, err
	}

	var enrollment MFAEnrollment
	err = s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		row, err := q.LockChallenge(ctx, sqlcgen.LockChallengeParams{TenantID: tenantID, TokenHash: security.HashToken(secret)})
		if errors.Is(err, pgx.ErrNoRows) {
			return challengeExpired()
		}
		if err != nil {
			return fmt.Errorf("lock challenge: %w", err)
		}
		user, err := q.GetUserByID(ctx, sqlcgen.GetUserByIDParams{TenantID: tenantID, ID: row.UserID})
		if err != nil {
			return fmt.Errorf("load user: %w", err)
		}
		enrollment, err = s.prepareEnrollment(ctx, q, user)
		return err
	})
	return enrollment, err
}

// ConfirmRequiredEnrollment activates the authenticator and starts the session.
func (s *Service) ConfirmRequiredEnrollment(ctx context.Context, portal Portal, challenge, code string, client Client) (AuthResult, error) {
	if !portal.MFARequired {
		return AuthResult{}, challengeExpired()
	}
	var result AuthResult
	err := s.withChallenge(ctx, portal, challenge, func(tx pgx.Tx, q *sqlcgen.Queries, user sqlcgen.IdentityUser) (bool, error) {
		if user.MfaEnabledAt != nil {
			return false, nil
		}
		accepted, err := s.matchCode(ctx, q, user, code)
		if err != nil || !accepted {
			return false, err
		}
		codes, err := activateMFA(ctx, tx, q, user)
		if err != nil {
			return false, err
		}
		result, err = s.startSession(ctx, q, portal, user, client)
		result.RecoveryCodes = codes
		return err == nil, err
	})
	return result, err
}

func (s *Service) loadPrincipalUser(ctx context.Context, principal authz.Principal) (sqlcgen.IdentityUser, error) {
	var user sqlcgen.IdentityUser
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		var err error
		user, err = q.GetUserByID(ctx, sqlcgen.GetUserByIDParams{TenantID: principal.TenantID, ID: principal.UserID})
		return err
	})
	if err != nil {
		return sqlcgen.IdentityUser{}, fmt.Errorf("load user: %w", err)
	}
	return user, nil
}

func (s *Service) confirmPassword(ctx context.Context, user sqlcgen.IdentityUser, password string) error {
	if user.PasswordHash == nil {
		return invalidCredentials()
	}
	valid, err := s.hasher.Verify(ctx, password, *user.PasswordHash)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !valid {
		return apperr.New(apperr.KindInvalid, "INVALID_PASSWORD", "The current password is incorrect.")
	}
	return nil
}

// StartEnrollment begins two-step verification for a signed-in account.
func (s *Service) StartEnrollment(ctx context.Context, principal authz.Principal, password string) (MFAEnrollment, error) {
	user, err := s.loadPrincipalUser(ctx, principal)
	if err != nil {
		return MFAEnrollment{}, err
	}
	if err := s.confirmPassword(ctx, user, password); err != nil {
		return MFAEnrollment{}, err
	}
	var enrollment MFAEnrollment
	err = s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		var err error
		enrollment, err = s.prepareEnrollment(ctx, q, user)
		return err
	})
	return enrollment, err
}

// ConfirmEnrollment activates two-step verification and ends other sessions.
func (s *Service) ConfirmEnrollment(ctx context.Context, principal authz.Principal, code string) ([]string, error) {
	user, err := s.loadPrincipalUser(ctx, principal)
	if err != nil {
		return nil, err
	}
	if user.MfaEnabledAt != nil {
		return nil, apperr.New(apperr.KindConflict, "MFA_ALREADY_ENABLED", "Two-step verification is already enabled.")
	}
	var codes []string
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		accepted, err := s.matchCode(ctx, q, user, code)
		if err != nil {
			return err
		}
		if !accepted {
			return invalidCode()
		}
		codes, err = activateMFA(ctx, tx, q, user)
		if err != nil {
			return err
		}
		reason := "mfa_enabled"
		_, err = q.RevokeOtherSessions(ctx, sqlcgen.RevokeOtherSessionsParams{
			TenantID: user.TenantID, UserID: user.ID, ID: principal.SessionID, RevokedReason: &reason,
		})
		return err
	})
	return codes, err
}

// DisableMFA removes two-step verification after confirming both factors.
func (s *Service) DisableMFA(ctx context.Context, portal Portal, principal authz.Principal, password, code string) error {
	if portal.MFARequired {
		return apperr.New(apperr.KindForbidden, "MFA_REQUIRED", "Two-step verification is mandatory for this account.")
	}
	user, err := s.loadPrincipalUser(ctx, principal)
	if err != nil {
		return err
	}
	if err := s.confirmPassword(ctx, user, password); err != nil {
		return err
	}
	return s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		accepted, err := s.matchCodeOrRecovery(ctx, q, user, code)
		if err != nil {
			return err
		}
		if !accepted {
			return invalidCode()
		}
		if err := q.DisableMFA(ctx, sqlcgen.DisableMFAParams{TenantID: user.TenantID, ID: user.ID}); err != nil {
			return fmt.Errorf("disable mfa: %w", err)
		}
		if err := q.DeleteRecoveryCodes(ctx, sqlcgen.DeleteRecoveryCodesParams{TenantID: user.TenantID, UserID: user.ID}); err != nil {
			return fmt.Errorf("delete recovery codes: %w", err)
		}
		return selfAudit(ctx, tx, user.TenantID, user.ID, "identity.mfa.disabled")
	})
}

// RegenerateRecoveryCodes replaces the recovery codes of an account.
func (s *Service) RegenerateRecoveryCodes(ctx context.Context, principal authz.Principal, code string) ([]string, error) {
	user, err := s.loadPrincipalUser(ctx, principal)
	if err != nil {
		return nil, err
	}
	if user.MfaEnabledAt == nil {
		return nil, apperr.New(apperr.KindConflict, "MFA_NOT_ENABLED", "Two-step verification is not enabled.")
	}
	var codes []string
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		accepted, err := s.matchCode(ctx, q, user, code)
		if err != nil {
			return err
		}
		if !accepted {
			return invalidCode()
		}
		codes, err = replaceRecoveryCodes(ctx, q, user)
		if err != nil {
			return err
		}
		return selfAudit(ctx, tx, user.TenantID, user.ID, "identity.mfa.recovery_codes_replaced")
	})
	return codes, err
}
