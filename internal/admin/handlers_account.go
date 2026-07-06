package admin

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/xiqi/wispbox/internal/api/httpjson"
	"github.com/xiqi/wispbox/internal/auth"
	"github.com/xiqi/wispbox/internal/branding"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/security"
)

const (
	adminTOTPSetupKind       = "admin_totp_setup"
	adminPasskeyRegisterKind = "admin_passkey_register"
	adminPasskeyLoginKind    = "admin_passkey_login"
)

func (h *Handlers) accountSecurity(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	passkeys, err := h.Core.Store.ListPasskeys(r.Context(), db.UserAdmin, ac.Admin.ID, "")
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	if passkeys == nil {
		passkeys = []db.Passkey{}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{
		"two_factor_enabled": ac.Admin.TwoFactorEnabled,
		"passkeys":           passkeys,
	})
}

func (h *Handlers) changePassword(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if !auth.VerifyAdminPassword(req.CurrentPassword, ac.Admin.PasswordHash) {
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("current password is wrong"))
		return
	}
	hash, err := auth.HashAdminPassword(req.NewPassword)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	if err := h.Core.Store.UpdateAdminPassword(r.Context(), ac.Admin.ID, hash); err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "admin_password_change", "admin", ac.Admin.Username)
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) totpSetup(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	secret, uri, err := auth.GenerateTOTPSecret(branding.CurrentForHost(r.Context(), h.Core.Store, r.Host).Name, ac.Admin.Username)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	encrypted, err := security.Encrypt(h.Core.Secret, secret)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	challengeID, err := h.Core.Store.CreateAuthChallenge(r.Context(), db.UserAdmin, ac.Admin.ID, adminTOTPSetupKind, encrypted, auth.PasskeyChallengeTTL)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"challenge_id": challengeID, "secret": secret, "otpauth_uri": uri})
}

func (h *Handlers) totpEnable(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	var req struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	chal, err := h.Core.Store.GetAuthChallenge(r.Context(), req.ChallengeID, adminTOTPSetupKind)
	if err != nil || chal.UserType != db.UserAdmin || chal.UserID != ac.Admin.ID {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("verification setup expired; start again"))
		return
	}
	secret, err := security.Decrypt(h.Core.Secret, chal.SessionData)
	if err != nil || !auth.ValidateTOTP(secret, req.Code) {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("verification code is wrong"))
		return
	}
	if err := h.Core.Store.UpdateAdminTOTP(r.Context(), ac.Admin.ID, true, chal.SessionData); err != nil {
		httpjson.Fail(w, err)
		return
	}
	_ = h.Core.Store.DeleteAuthChallenge(r.Context(), chal.ID)
	h.auditLog(r, ac, "admin_2fa_enable", "admin", ac.Admin.Username)
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) totpDisable(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	var req struct {
		Code string `json:"code"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if ac.Admin.TwoFactorEnabled {
		secret, err := security.Decrypt(h.Core.Secret, ac.Admin.EncryptedTOTPSecret)
		if err != nil || !auth.ValidateTOTP(secret, req.Code) {
			httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("verification code is wrong"))
			return
		}
	}
	if err := h.Core.Store.UpdateAdminTOTP(r.Context(), ac.Admin.ID, false, ""); err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "admin_2fa_disable", "admin", ac.Admin.Username)
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) passkeyRegisterOptions(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	wa, rpID, err := auth.WebAuthnForRequest(r, branding.CurrentForHost(r.Context(), h.Core.Store, r.Host).Name)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	user, err := auth.LoadWebAuthnUser(r.Context(), h.Core.Store, h.Core.Secret, db.UserAdmin, ac.Admin.ID, rpID, ac.Admin.Username, ac.Admin.Username)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	creation, session, err := wa.BeginRegistration(
		user,
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		}),
		webauthn.WithExclusions(webauthn.Credentials(user.Credentials).CredentialDescriptors()),
	)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	raw, _ := json.Marshal(session)
	challengeID, err := h.Core.Store.CreateAuthChallenge(r.Context(), db.UserAdmin, ac.Admin.ID, adminPasskeyRegisterKind, string(raw), auth.PasskeyChallengeTTL)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"challenge_id": challengeID, "options": creation.Response})
}

func (h *Handlers) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	challengeID := r.URL.Query().Get("challenge_id")
	chal, err := h.Core.Store.GetAuthChallenge(r.Context(), challengeID, adminPasskeyRegisterKind)
	if err != nil || chal.UserType != db.UserAdmin || chal.UserID != ac.Admin.ID {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("passkey setup expired; start again"))
		return
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(chal.SessionData), &session); err != nil {
		httpjson.Fail(w, err)
		return
	}
	wa, rpID, err := auth.WebAuthnForRequest(r, branding.CurrentForHost(r.Context(), h.Core.Store, r.Host).Name)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	user, err := auth.LoadWebAuthnUser(r.Context(), h.Core.Store, h.Core.Secret, db.UserAdmin, ac.Admin.ID, rpID, ac.Admin.Username, ac.Admin.Username)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	credential, err := wa.FinishRegistration(user, session, r)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("passkey registration failed"))
		return
	}
	encrypted, err := auth.EncryptCredential(h.Core.Secret, *credential)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	passkey, err := h.Core.Store.SavePasskey(r.Context(), db.Passkey{
		UserType: db.UserAdmin, UserID: ac.Admin.ID, RPID: rpID,
		CredentialID: auth.EncodeCredentialID(credential.ID),
		Name:         "Admin passkey", EncryptedCredential: encrypted,
	})
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	_ = h.Core.Store.DeleteAuthChallenge(r.Context(), chal.ID)
	h.auditLog(r, ac, "admin_passkey_add", "passkey", strconv.FormatInt(passkey.ID, 10))
	httpjson.Write(w, http.StatusCreated, map[string]any{"passkey": passkey})
}

func (h *Handlers) passkeyDelete(w http.ResponseWriter, r *http.Request, ac *adminCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := h.Core.Store.DeletePasskey(r.Context(), db.UserAdmin, ac.Admin.ID, id); err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.auditLog(r, ac, "admin_passkey_delete", "passkey", strconv.FormatInt(id, 10))
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) passkeyLoginOptions(w http.ResponseWriter, r *http.Request) {
	wa, _, err := auth.WebAuthnForRequest(r, branding.CurrentForHost(r.Context(), h.Core.Store, r.Host).Name)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	assertion, session, err := wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	raw, _ := json.Marshal(session)
	challengeID, err := h.Core.Store.CreateAuthChallenge(r.Context(), db.UserAdmin, 0, adminPasskeyLoginKind, string(raw), auth.PasskeyChallengeTTL)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"challenge_id": challengeID, "options": assertion.Response})
}

func (h *Handlers) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	challengeID := r.URL.Query().Get("challenge_id")
	chal, err := h.Core.Store.GetAuthChallenge(r.Context(), challengeID, adminPasskeyLoginKind)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("passkey sign-in expired; try again"))
		return
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(chal.SessionData), &session); err != nil {
		httpjson.Fail(w, err)
		return
	}
	wa, rpID, err := auth.WebAuthnForRequest(r, branding.CurrentForHost(r.Context(), h.Core.Store, r.Host).Name)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	loader := func(rawID, userHandle []byte) (webauthn.User, error) {
		p, err := h.Core.Store.GetPasskeyByCredential(r.Context(), rpID, auth.EncodeCredentialID(rawID))
		if err != nil || p.UserType != db.UserAdmin {
			return nil, fmt.Errorf("passkey not found")
		}
		adm, err := h.Core.Store.GetAdmin(r.Context(), p.UserID)
		if err != nil || adm.WebAuthnHandle != auth.EncodeCredentialID(userHandle) {
			return nil, fmt.Errorf("passkey not found")
		}
		return auth.LoadWebAuthnUser(r.Context(), h.Core.Store, h.Core.Secret, db.UserAdmin, adm.ID, rpID, adm.Username, adm.Username)
	}
	validatedUser, validatedCredential, err := wa.FinishPasskeyLogin(loader, session, r)
	if err != nil {
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("passkey sign-in failed"))
		return
	}
	user, ok := validatedUser.(*auth.WebAuthnUser)
	if !ok || user.UserType != db.UserAdmin {
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("passkey sign-in failed"))
		return
	}
	adm, err := h.Core.Store.GetAdmin(r.Context(), user.UserID)
	if err != nil {
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("passkey sign-in failed"))
		return
	}
	encrypted, err := auth.EncryptCredential(h.Core.Secret, *validatedCredential)
	if err == nil {
		if p, perr := h.Core.Store.GetPasskeyByCredential(r.Context(), rpID, auth.EncodeCredentialID(validatedCredential.ID)); perr == nil {
			_ = h.Core.Store.UpdatePasskeyCredential(r.Context(), p.ID, encrypted)
		}
	}
	csrf, err := h.Sessions.Login(r.Context(), w, db.UserAdmin, adm.ID, nil)
	if err != nil {
		httpjson.Error(w, http.StatusInternalServerError, fmt.Errorf("could not create session"))
		return
	}
	_ = h.Core.Store.DeleteAuthChallenge(r.Context(), chal.ID)
	_ = h.Core.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "admin", ActorID: adm.ID, Action: "passkey_login", TargetType: "admin", TargetID: adm.Username, IP: httpjson.ClientIP(r),
	})
	httpjson.Write(w, http.StatusOK, map[string]any{"username": adm.Username, "csrf": csrf})
}
