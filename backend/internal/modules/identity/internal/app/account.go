package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/audit"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/domain"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/modules/identity/internal/sqlcgen"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/authz"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/mail"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

// SessionView is one signed-in device.
type SessionView struct {
	ID         uuid.UUID `json:"id"`
	Device     string    `json:"device"`
	IP         string    `json:"ip"`
	CreatedAt  time.Time `json:"createdAt"`
	LastUsedAt time.Time `json:"lastUsedAt"`
	Current    bool      `json:"current"`
}

// AccessEventView is one entry of the access history.
type AccessEventView struct {
	ID         uuid.UUID `json:"id"`
	Event      string    `json:"event"`
	Device     string    `json:"device"`
	IP         string    `json:"ip"`
	OccurredAt time.Time `json:"occurredAt"`
}

// TeamMember is one account of a store or of the platform team.
type TeamMember struct {
	ID         uuid.UUID `json:"id"`
	Email      string    `json:"email"`
	Name       string    `json:"name"`
	Status     string    `json:"status"`
	MFAEnabled bool      `json:"mfaEnabled"`
	Role       string    `json:"role"`
	RoleName   string    `json:"roleName"`
}

// RoleView is a role that can be assigned.
type RoleView struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Me returns the account of the caller.
func (s *Service) Me(ctx context.Context, principal authz.Principal) (*UserView, error) {
	var view *UserView
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		user, err := q.GetUserByID(ctx, sqlcgen.GetUserByIDParams{TenantID: principal.TenantID, ID: principal.UserID})
		if err != nil {
			return fmt.Errorf("load user: %w", err)
		}
		view, err = s.userView(ctx, q, user)
		return err
	})
	return view, err
}

// ChangePassword replaces the password and ends the other sessions.
func (s *Service) ChangePassword(ctx context.Context, principal authz.Principal, current, next string) error {
	user, err := s.loadPrincipalUser(ctx, principal)
	if err != nil {
		return err
	}
	if err := s.confirmPassword(ctx, user, current); err != nil {
		return err
	}
	if err := domain.ValidatePassword(next, user.Email); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(ctx, next)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	return s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		if err := q.UpdatePassword(ctx, sqlcgen.UpdatePasswordParams{TenantID: user.TenantID, ID: user.ID, PasswordHash: &hash}); err != nil {
			return fmt.Errorf("update password: %w", err)
		}
		reason := "password_changed"
		if _, err := q.RevokeOtherSessions(ctx, sqlcgen.RevokeOtherSessionsParams{
			TenantID: user.TenantID, UserID: user.ID, ID: principal.SessionID, RevokedReason: &reason,
		}); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		return selfAudit(ctx, tx, user.TenantID, user.ID, "identity.password.changed")
	})
}

// ListSessions returns the active sessions of the caller.
func (s *Service) ListSessions(ctx context.Context, principal authz.Principal) ([]SessionView, error) {
	views := []SessionView{}
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		rows, err := q.ListActiveSessions(ctx, sqlcgen.ListActiveSessionsParams{TenantID: principal.TenantID, UserID: principal.UserID})
		if err != nil {
			return fmt.Errorf("list sessions: %w", err)
		}
		for _, row := range rows {
			views = append(views, SessionView{
				ID: row.ID, Device: row.DeviceLabel, IP: row.Ip, CreatedAt: row.CreatedAt,
				LastUsedAt: row.LastUsedAt, Current: row.ID == principal.SessionID,
			})
		}
		return nil
	})
	return views, err
}

// RevokeSession ends one session. Sessions of other accounts look nonexistent.
func (s *Service) RevokeSession(ctx context.Context, principal authz.Principal, sessionID uuid.UUID) error {
	return s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		reason := "revoked_by_user"
		affected, err := q.RevokeSession(ctx, sqlcgen.RevokeSessionParams{
			TenantID: principal.TenantID, UserID: principal.UserID, ID: sessionID, RevokedReason: &reason,
		})
		if err != nil {
			return fmt.Errorf("revoke session: %w", err)
		}
		if affected == 0 {
			return apperr.New(apperr.KindNotFound, "SESSION_NOT_FOUND", "The session does not exist.")
		}
		return nil
	})
}

// RevokeOtherSessions ends every session except the current one.
func (s *Service) RevokeOtherSessions(ctx context.Context, principal authz.Principal) error {
	return s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		reason := "revoked_by_user"
		_, err := q.RevokeOtherSessions(ctx, sqlcgen.RevokeOtherSessionsParams{
			TenantID: principal.TenantID, UserID: principal.UserID, ID: principal.SessionID, RevokedReason: &reason,
		})
		return err
	})
}

// AccessHistory returns the recent sign-in activity of the caller.
func (s *Service) AccessHistory(ctx context.Context, principal authz.Principal) ([]AccessEventView, error) {
	views := []AccessEventView{}
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		rows, err := q.ListAccessEvents(ctx, sqlcgen.ListAccessEventsParams{
			TenantID: principal.TenantID, UserID: &principal.UserID, Limit: historyLimit,
		})
		if err != nil {
			return fmt.Errorf("list access events: %w", err)
		}
		for _, row := range rows {
			views = append(views, AccessEventView{ID: row.ID, Event: row.Event, Device: row.Device, IP: row.Ip, OccurredAt: row.OccurredAt})
		}
		return nil
	})
	return views, err
}

// ListRoles returns the roles of a portal.
func (s *Service) ListRoles(ctx context.Context, portal Portal) ([]RoleView, error) {
	views := []RoleView{}
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		rows, err := q.ListRoles(ctx, portal.Realm)
		if err != nil {
			return fmt.Errorf("list roles: %w", err)
		}
		for _, row := range rows {
			views = append(views, RoleView{Code: row.Code, Name: row.Name})
		}
		return nil
	})
	return views, err
}

// ListTeam returns the team of the tenant of the caller.
func (s *Service) ListTeam(ctx context.Context, portal Portal, principal authz.Principal) ([]TeamMember, error) {
	members := []TeamMember{}
	err := s.in(ctx, func(_ pgx.Tx, q *sqlcgen.Queries) error {
		rows, err := q.ListTeam(ctx, sqlcgen.ListTeamParams{TenantID: principal.TenantID, Realm: portal.Realm})
		if err != nil {
			return fmt.Errorf("list team: %w", err)
		}
		for _, row := range rows {
			members = append(members, TeamMember{
				ID: row.ID, Email: row.Email, Name: row.Name, Status: row.Status,
				MFAEnabled: row.MfaEnabledAt != nil, Role: row.RoleCode, RoleName: row.RoleName,
			})
		}
		return nil
	})
	return members, err
}

func roleOf(ctx context.Context, q *sqlcgen.Queries, portal Portal, code string) (sqlcgen.IdentityRole, error) {
	role, err := q.GetRole(ctx, code)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role.Realm != portal.Realm) {
		return sqlcgen.IdentityRole{}, apperr.Validation(apperr.FieldError{Field: "role", Code: "invalid", Message: "Choose a valid role."})
	}
	if err != nil {
		return sqlcgen.IdentityRole{}, fmt.Errorf("load role: %w", err)
	}
	return role, nil
}

func teamMember(ctx context.Context, q *sqlcgen.Queries, portal Portal, tenantID, userID uuid.UUID) (sqlcgen.IdentityUser, error) {
	user, err := q.GetUserByID(ctx, sqlcgen.GetUserByIDParams{TenantID: tenantID, ID: userID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && user.Realm != portal.Realm) {
		return sqlcgen.IdentityUser{}, apperr.New(apperr.KindNotFound, "MEMBER_NOT_FOUND", "The team member does not exist.")
	}
	if err != nil {
		return sqlcgen.IdentityUser{}, fmt.Errorf("load member: %w", err)
	}
	return user, nil
}

// protectLastAdmin keeps a tenant from losing its last top administrator. The
// rows are locked so two simultaneous changes cannot both pass this check.
func protectLastAdmin(ctx context.Context, q *sqlcgen.Queries, portal Portal, tenantID, targetID uuid.UUID) error {
	admins, err := q.LockActiveTopAdmins(ctx, sqlcgen.LockActiveTopAdminsParams{TenantID: tenantID, RoleCode: portal.TopRole})
	if err != nil {
		return fmt.Errorf("lock administrators: %w", err)
	}
	if len(admins) == 1 && admins[0] == targetID {
		return apperr.New(apperr.KindConflict, "LAST_ADMIN", "The team must keep at least one active administrator.")
	}
	return nil
}

func selfChange() error {
	return apperr.New(apperr.KindConflict, "SELF_CHANGE_NOT_ALLOWED", "Ask another administrator to make this change.")
}

// InviteMember creates an invited account and sends the invitation by e-mail.
func (s *Service) InviteMember(ctx context.Context, portal Portal, principal authz.Principal, rawName, rawEmail, roleCode, linkBase string) error {
	name, err := domain.NormalizeName(rawName)
	if err != nil {
		return err
	}
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		return err
	}
	var token string
	err = s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		if _, err := roleOf(ctx, q, portal, roleCode); err != nil {
			return err
		}
		userID := newID()
		if err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
			TenantID: principal.TenantID, ID: userID, Realm: portal.Realm, Email: email, Name: name, Status: domain.StatusInvited,
		}); err != nil {
			return err
		}
		if err := q.SetUserRole(ctx, sqlcgen.SetUserRoleParams{TenantID: principal.TenantID, UserID: userID, RoleCode: roleCode}); err != nil {
			return fmt.Errorf("set role: %w", err)
		}
		token, err = createUserToken(ctx, q, principal.TenantID, userID, domain.TokenInvitation, s.now().Add(invitationTTL))
		if err != nil {
			return err
		}
		return userAudit(ctx, tx, principal, "identity.member.invited", "user", userID.String(), map[string]string{"role": roleCode})
	})
	if isUniqueViolation(err) {
		return apperr.New(apperr.KindConflict, "MEMBER_ALREADY_EXISTS", "This e-mail is already on the team.")
	}
	if err != nil {
		return err
	}
	s.mailer.Enqueue(ctx, mail.Message{
		Kind: "identity.invitation", To: email, Subject: "Convite para a equipe",
		Body: "Você foi convidado para a equipe. Crie sua senha no endereço abaixo, válido por 7 dias.\n\n" +
			linkBase + "/convite?token=" + token,
	})
	return nil
}

// ChangeMemberRole assigns another role to a team member.
func (s *Service) ChangeMemberRole(ctx context.Context, portal Portal, principal authz.Principal, userID uuid.UUID, roleCode string) error {
	if userID == principal.UserID {
		return selfChange()
	}
	return s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		if _, err := roleOf(ctx, q, portal, roleCode); err != nil {
			return err
		}
		if _, err := teamMember(ctx, q, portal, principal.TenantID, userID); err != nil {
			return err
		}
		if roleCode != portal.TopRole {
			if err := protectLastAdmin(ctx, q, portal, principal.TenantID, userID); err != nil {
				return err
			}
		}
		if err := q.SetUserRole(ctx, sqlcgen.SetUserRoleParams{TenantID: principal.TenantID, UserID: userID, RoleCode: roleCode}); err != nil {
			return fmt.Errorf("set role: %w", err)
		}
		return userAudit(ctx, tx, principal, "identity.member.role_changed", "user", userID.String(), map[string]string{"role": roleCode})
	})
}

// DeactivateMember blocks an account and ends its sessions immediately.
func (s *Service) DeactivateMember(ctx context.Context, portal Portal, principal authz.Principal, userID uuid.UUID) error {
	if userID == principal.UserID {
		return selfChange()
	}
	return s.in(ctx, func(tx pgx.Tx, q *sqlcgen.Queries) error {
		if _, err := teamMember(ctx, q, portal, principal.TenantID, userID); err != nil {
			return err
		}
		if err := protectLastAdmin(ctx, q, portal, principal.TenantID, userID); err != nil {
			return err
		}
		if err := q.SetUserStatus(ctx, sqlcgen.SetUserStatusParams{TenantID: principal.TenantID, ID: userID, Status: domain.StatusDisabled}); err != nil {
			return fmt.Errorf("disable user: %w", err)
		}
		reason := "member_deactivated"
		if _, err := q.RevokeAllSessions(ctx, sqlcgen.RevokeAllSessionsParams{TenantID: principal.TenantID, UserID: userID, RevokedReason: &reason}); err != nil {
			return fmt.Errorf("revoke sessions: %w", err)
		}
		return userAudit(ctx, tx, principal, "identity.member.deactivated", "user", userID.String(), nil)
	})
}

// ListAudit returns the recent administrative operations of the tenant.
func (s *Service) ListAudit(ctx context.Context, principal authz.Principal) ([]audit.View, error) {
	var views []audit.View
	err := s.in(ctx, func(tx pgx.Tx, _ *sqlcgen.Queries) error {
		var err error
		views, err = audit.List(ctx, tx, principal.TenantID, auditLimit)
		return err
	})
	return views, err
}

// Deliver sends an invitation inside the transaction that creates it, so the
// account exists only if the message was stored.
type Deliver func(ctx context.Context, tx pgx.Tx, email, token string) error

// OperatorInvitation creates the first accounts of a tenant on behalf of a
// platform operator. The pool must use the platform role. A nil deliver
// leaves the delivery to the caller, which receives the token.
func OperatorInvitation(ctx context.Context, pool *pgxpool.Pool, portal Portal, tenantID uuid.UUID, rawName, rawEmail, roleCode string, deliver Deliver) (string, error) {
	name, err := domain.NormalizeName(rawName)
	if err != nil {
		return "", err
	}
	email, err := domain.NormalizeEmail(rawEmail)
	if err != nil {
		return "", err
	}
	var token string
	err = pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		q := sqlcgen.New(tx)
		if _, err := roleOf(ctx, q, portal, roleCode); err != nil {
			return err
		}
		userID := newID()
		if err := q.CreateUser(ctx, sqlcgen.CreateUserParams{
			TenantID: tenantID, ID: userID, Realm: portal.Realm, Email: email, Name: name, Status: domain.StatusInvited,
		}); err != nil {
			return err
		}
		if err := q.SetUserRole(ctx, sqlcgen.SetUserRoleParams{TenantID: tenantID, UserID: userID, RoleCode: roleCode}); err != nil {
			return fmt.Errorf("set role: %w", err)
		}
		token, err = createUserToken(ctx, q, tenantID, userID, domain.TokenInvitation, time.Now().Add(invitationTTL))
		if err != nil {
			return err
		}
		if deliver != nil {
			if err := deliver(ctx, tx, email, token); err != nil {
				return err
			}
		}
		return audit.Record(ctx, tx, audit.Entry{
			TenantID: tenantID, ActorType: audit.ActorOperator, ActorID: audit.OperatorFrom(ctx),
			Action: "identity.member.invited", TargetType: "user", TargetID: userID.String(),
			Metadata: map[string]string{"role": roleCode},
		})
	})
	if isUniqueViolation(err) {
		return "", apperr.New(apperr.KindConflict, "MEMBER_ALREADY_EXISTS", "This e-mail is already on the team.")
	}
	return token, err
}
