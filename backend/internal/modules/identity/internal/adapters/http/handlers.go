package http

import (
	"net/http"

	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/httpx"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/platform/tenancy"
	"github.com/will-ferr/Ecommerce-White-Label/backend/internal/shared/apperr"
)

func noContent(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func accepted(w http.ResponseWriter) error {
	w.WriteHeader(http.StatusAccepted)
	return nil
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitIP(r, "register", registerByIP); err != nil {
		return err
	}
	linkBase := h.service.LinkBase(h.portal, hostname(r.Host))
	if err := h.service.Register(r.Context(), h.portal, body.Name, body.Email, body.Password, linkBase); err != nil {
		return err
	}
	return accepted(w)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Store    string `json:"store"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitIP(r, "login", loginByIP); err != nil {
		return err
	}
	ctx, err := h.scopeTenant(r.Context(), body.Store)
	if err != nil {
		return err
	}
	tenantID, ok := tenancy.FromContext(ctx)
	if !ok {
		return tenancy.ErrMissingTenant
	}
	if err := h.limit(ctx, "login:account", accountKey(tenantID, body.Email), loginByAccount); err != nil {
		return err
	}
	result, err := h.service.Login(ctx, h.portal, body.Email, body.Password, client(r))
	if err != nil {
		return err
	}
	return h.respondAuth(w, result)
}

type challengeBody struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

func (h *Handler) mfaChallenge(w http.ResponseWriter, r *http.Request) error {
	var body challengeBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitIP(r, "mfa", mfaByIP); err != nil {
		return err
	}
	result, err := h.service.CompleteChallenge(r.Context(), h.portal, body.Challenge, body.Code, client(r))
	if err != nil {
		return err
	}
	return h.respondAuth(w, result)
}

func (h *Handler) mfaSetupStart(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Challenge string `json:"challenge"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitIP(r, "mfa", mfaByIP); err != nil {
		return err
	}
	enrollment, err := h.service.StartRequiredEnrollment(r.Context(), h.portal, body.Challenge)
	if err != nil {
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, enrollment)
}

func (h *Handler) mfaSetupConfirm(w http.ResponseWriter, r *http.Request) error {
	var body challengeBody
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitIP(r, "mfa", mfaByIP); err != nil {
		return err
	}
	result, err := h.service.ConfirmRequiredEnrollment(r.Context(), h.portal, body.Challenge, body.Code, client(r))
	if err != nil {
		return err
	}
	return h.respondAuth(w, result)
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) error {
	if err := requireSameOrigin(r); err != nil {
		return err
	}
	if err := h.limitIP(r, "refresh", refreshByIP); err != nil {
		return err
	}
	cookie, err := r.Cookie(h.cookieName())
	if err != nil {
		return apperr.New(apperr.KindUnauthenticated, "SESSION_EXPIRED", "Your session has expired. Sign in again.")
	}
	result, err := h.service.Refresh(r.Context(), h.portal, cookie.Value, client(r))
	if err != nil {
		if appErr, ok := apperr.As(err); ok && appErr.Code == "SESSION_EXPIRED" {
			h.clearRefreshCookie(w)
		}
		return err
	}
	return h.respondAuth(w, result)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) error {
	if err := requireSameOrigin(r); err != nil {
		return err
	}
	if cookie, err := r.Cookie(h.cookieName()); err == nil {
		if err := h.service.Logout(r.Context(), h.portal, cookie.Value); err != nil {
			return err
		}
	}
	h.clearRefreshCookie(w)
	return noContent(w)
}

func (h *Handler) forgotPassword(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Store string `json:"store"`
		Email string `json:"email"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitIP(r, "forgot", forgotByIP); err != nil {
		return err
	}
	ctx, err := h.scopeTenant(r.Context(), body.Store)
	if err != nil {
		// An unknown store gets the same answer as an unknown account.
		if appErr, ok := apperr.As(err); ok && appErr.Code == "INVALID_CREDENTIALS" {
			return accepted(w)
		}
		return err
	}
	tenantID, ok := tenancy.FromContext(ctx)
	if !ok {
		return tenancy.ErrMissingTenant
	}
	if err := h.limit(ctx, "forgot:account", accountKey(tenantID, body.Email), forgotByAccount); err != nil {
		return accepted(w) //nolint:nilerr // A limited account gets the same answer as any other.
	}
	linkBase := h.service.LinkBase(h.portal, hostname(r.Host))
	if err := h.service.ForgotPassword(ctx, h.portal, body.Email, linkBase); err != nil {
		return err
	}
	return accepted(w)
}

func (h *Handler) resetPassword(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitIP(r, "token", tokenByIP); err != nil {
		return err
	}
	if err := h.service.ResetPassword(r.Context(), h.portal, body.Token, body.Password); err != nil {
		return err
	}
	return noContent(w)
}

func (h *Handler) verifyEmail(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Token string `json:"token"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitIP(r, "token", tokenByIP); err != nil {
		return err
	}
	if err := h.service.VerifyEmail(r.Context(), h.portal, body.Token); err != nil {
		return err
	}
	return noContent(w)
}

func (h *Handler) acceptInvitation(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Token    string `json:"token"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitIP(r, "token", tokenByIP); err != nil {
		return err
	}
	if err := h.service.AcceptInvitation(r.Context(), h.portal, body.Token, body.Name, body.Password); err != nil {
		return err
	}
	return noContent(w)
}

func (h *Handler) me(w http.ResponseWriter, r *http.Request) error {
	view, err := h.service.Me(r.Context(), principalOf(r))
	if err != nil {
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, view)
}

func (h *Handler) limitUser(r *http.Request, scope string) error {
	return h.limit(r.Context(), scope+":user", principalOf(r).UserID.String(), passwordByUser)
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitUser(r, "password"); err != nil {
		return err
	}
	if err := h.service.ChangePassword(r.Context(), principalOf(r), body.CurrentPassword, body.NewPassword); err != nil {
		return err
	}
	return noContent(w)
}

func (h *Handler) listSessions(w http.ResponseWriter, r *http.Request) error {
	sessions, err := h.service.ListSessions(r.Context(), principalOf(r))
	if err != nil {
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": sessions})
}

func (h *Handler) revokeSession(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := h.service.RevokeSession(r.Context(), principalOf(r), id); err != nil {
		return err
	}
	return noContent(w)
}

func (h *Handler) revokeOtherSessions(w http.ResponseWriter, r *http.Request) error {
	if err := h.service.RevokeOtherSessions(r.Context(), principalOf(r)); err != nil {
		return err
	}
	return noContent(w)
}

func (h *Handler) accessHistory(w http.ResponseWriter, r *http.Request) error {
	events, err := h.service.AccessHistory(r.Context(), principalOf(r))
	if err != nil {
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": events})
}

func (h *Handler) mfaEnroll(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Password string `json:"password"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitUser(r, "mfa"); err != nil {
		return err
	}
	enrollment, err := h.service.StartEnrollment(r.Context(), principalOf(r), body.Password)
	if err != nil {
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, enrollment)
}

func (h *Handler) mfaEnrollConfirm(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Code string `json:"code"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitUser(r, "mfa"); err != nil {
		return err
	}
	codes, err := h.service.ConfirmEnrollment(r.Context(), principalOf(r), body.Code)
	if err != nil {
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

func (h *Handler) mfaDisable(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitUser(r, "mfa"); err != nil {
		return err
	}
	if err := h.service.DisableMFA(r.Context(), h.portal, principalOf(r), body.Password, body.Code); err != nil {
		return err
	}
	return noContent(w)
}

func (h *Handler) mfaRecoveryCodes(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Code string `json:"code"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.limitUser(r, "mfa"); err != nil {
		return err
	}
	codes, err := h.service.RegenerateRecoveryCodes(r.Context(), principalOf(r), body.Code)
	if err != nil {
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, map[string]any{"recoveryCodes": codes})
}

func (h *Handler) listRoles(w http.ResponseWriter, r *http.Request) error {
	roles, err := h.service.ListRoles(r.Context(), h.portal)
	if err != nil {
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": roles})
}

func (h *Handler) listTeam(w http.ResponseWriter, r *http.Request) error {
	members, err := h.service.ListTeam(r.Context(), h.portal, principalOf(r))
	if err != nil {
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": members})
}

func (h *Handler) inviteMember(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	linkBase := h.service.LinkBase(h.portal, hostname(r.Host))
	if err := h.service.InviteMember(r.Context(), h.portal, principalOf(r), body.Name, body.Email, body.Role, linkBase); err != nil {
		return err
	}
	return accepted(w)
}

func (h *Handler) changeRole(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	var body struct {
		Role string `json:"role"`
	}
	if err := httpx.DecodeJSON(r, &body); err != nil {
		return err
	}
	if err := h.service.ChangeMemberRole(r.Context(), h.portal, principalOf(r), id, body.Role); err != nil {
		return err
	}
	return noContent(w)
}

func (h *Handler) deactivateMember(w http.ResponseWriter, r *http.Request) error {
	id, err := pathID(r)
	if err != nil {
		return err
	}
	if err := h.service.DeactivateMember(r.Context(), h.portal, principalOf(r), id); err != nil {
		return err
	}
	return noContent(w)
}

func (h *Handler) listAudit(w http.ResponseWriter, r *http.Request) error {
	entries, err := h.service.ListAudit(r.Context(), principalOf(r))
	if err != nil {
		return err
	}
	return httpx.WriteJSON(w, http.StatusOK, map[string]any{"items": entries})
}
