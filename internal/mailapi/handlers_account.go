package mailapi

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
	mailTOTPSetupKind       = "mail_totp_setup"
	mailPasskeyRegisterKind = "mail_passkey_register"
	mailPasskeyLoginKind    = "mail_passkey_login"
)

func (h *Handlers) accountSecurity(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	passkeys, err := h.Store.ListPasskeys(r.Context(), db.UserMailbox, mc.Mailbox.ID, "")
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	if passkeys == nil {
		passkeys = []db.Passkey{}
	}
	httpjson.Write(w, http.StatusOK, map[string]any{
		"two_factor_enabled": mc.Mailbox.TwoFactorEnabled,
		"passkeys":           passkeys,
	})
}

func (h *Handlers) changePassword(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if !auth.VerifyMailboxPassword(req.CurrentPassword, mc.Mailbox.PasswordHash) {
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("current password is wrong"))
		return
	}
	hash, err := auth.HashMailboxPassword(req.NewPassword)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	encryptedPassword, err := security.Encrypt(h.Secret, req.NewPassword)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	if err := h.Store.UpdateMailboxPasswordAndPasskeySecret(r.Context(), mc.Mailbox.ID, hash, encryptedPassword); err != nil {
		httpjson.Fail(w, err)
		return
	}
	h.Sessions.SetCredentials(mc.Session.ID, auth.Credentials{Email: mc.Mailbox.Email, Password: req.NewPassword})
	_ = h.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "mailbox", ActorID: mc.Mailbox.ID, Action: "mailbox_password_change", TargetType: "mailbox", TargetID: mc.Mailbox.Email, IP: httpjson.ClientIP(r),
	})
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) totpSetup(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	secret, uri, err := auth.GenerateTOTPSecret(branding.CurrentForHost(r.Context(), h.Store, r.Host).Name, mc.Mailbox.Email)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	encrypted, err := security.Encrypt(h.Secret, secret)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	challengeID, err := h.Store.CreateAuthChallenge(r.Context(), db.UserMailbox, mc.Mailbox.ID, mailTOTPSetupKind, encrypted, auth.PasskeyChallengeTTL)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"challenge_id": challengeID, "secret": secret, "otpauth_uri": uri})
}

func (h *Handlers) totpEnable(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	var req struct {
		ChallengeID string `json:"challenge_id"`
		Code        string `json:"code"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	chal, err := h.Store.GetAuthChallenge(r.Context(), req.ChallengeID, mailTOTPSetupKind)
	if err != nil || chal.UserType != db.UserMailbox || chal.UserID != mc.Mailbox.ID {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("verification setup expired; start again"))
		return
	}
	secret, err := security.Decrypt(h.Secret, chal.SessionData)
	if err != nil || !auth.ValidateTOTP(secret, req.Code) {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("verification code is wrong"))
		return
	}
	if err := h.Store.UpdateMailboxTOTP(r.Context(), mc.Mailbox.ID, true, chal.SessionData); err != nil {
		httpjson.Fail(w, err)
		return
	}
	_ = h.Store.DeleteAuthChallenge(r.Context(), chal.ID)
	_ = h.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "mailbox", ActorID: mc.Mailbox.ID, Action: "mailbox_2fa_enable", TargetType: "mailbox", TargetID: mc.Mailbox.Email, IP: httpjson.ClientIP(r),
	})
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) totpDisable(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	var req struct {
		Code string `json:"code"`
	}
	if err := httpjson.Decode(w, r, &req, 4096); err != nil {
		httpjson.Error(w, http.StatusBadRequest, err)
		return
	}
	if mc.Mailbox.TwoFactorEnabled {
		secret, err := security.Decrypt(h.Secret, mc.Mailbox.EncryptedTOTPSecret)
		if err != nil || !auth.ValidateTOTP(secret, req.Code) {
			httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("verification code is wrong"))
			return
		}
	}
	if err := h.Store.UpdateMailboxTOTP(r.Context(), mc.Mailbox.ID, false, ""); err != nil {
		httpjson.Fail(w, err)
		return
	}
	_ = h.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "mailbox", ActorID: mc.Mailbox.ID, Action: "mailbox_2fa_disable", TargetType: "mailbox", TargetID: mc.Mailbox.Email, IP: httpjson.ClientIP(r),
	})
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) passkeyRegisterOptions(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	if !auth.VerifyMailboxPassword(mc.Creds.Password, mc.Mailbox.PasswordHash) {
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("please sign in again"))
		return
	}
	encryptedPassword, err := security.Encrypt(h.Secret, mc.Creds.Password)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	hash := mc.Mailbox.PasswordHash
	if err := h.Store.UpdateMailboxPasswordAndPasskeySecret(r.Context(), mc.Mailbox.ID, hash, encryptedPassword); err != nil {
		httpjson.Fail(w, err)
		return
	}
	wa, rpID, err := auth.WebAuthnForRequest(r, branding.CurrentForHost(r.Context(), h.Store, r.Host).Name)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	user, err := auth.LoadWebAuthnUser(r.Context(), h.Store, h.Secret, db.UserMailbox, mc.Mailbox.ID, rpID, mc.Mailbox.Email, mc.Mailbox.Email)
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
	challengeID, err := h.Store.CreateAuthChallenge(r.Context(), db.UserMailbox, mc.Mailbox.ID, mailPasskeyRegisterKind, string(raw), auth.PasskeyChallengeTTL)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"challenge_id": challengeID, "options": creation.Response})
}

func (h *Handlers) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	challengeID := r.URL.Query().Get("challenge_id")
	chal, err := h.Store.GetAuthChallenge(r.Context(), challengeID, mailPasskeyRegisterKind)
	if err != nil || chal.UserType != db.UserMailbox || chal.UserID != mc.Mailbox.ID {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("passkey setup expired; start again"))
		return
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(chal.SessionData), &session); err != nil {
		httpjson.Fail(w, err)
		return
	}
	wa, rpID, err := auth.WebAuthnForRequest(r, branding.CurrentForHost(r.Context(), h.Store, r.Host).Name)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	user, err := auth.LoadWebAuthnUser(r.Context(), h.Store, h.Secret, db.UserMailbox, mc.Mailbox.ID, rpID, mc.Mailbox.Email, mc.Mailbox.Email)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	credential, err := wa.FinishRegistration(user, session, r)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("passkey registration failed"))
		return
	}
	encrypted, err := auth.EncryptCredential(h.Secret, *credential)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	passkey, err := h.Store.SavePasskey(r.Context(), db.Passkey{
		UserType: db.UserMailbox, UserID: mc.Mailbox.ID, RPID: rpID,
		CredentialID: auth.EncodeCredentialID(credential.ID),
		Name:         "Mailbox passkey", EncryptedCredential: encrypted,
	})
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	_ = h.Store.DeleteAuthChallenge(r.Context(), chal.ID)
	_ = h.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "mailbox", ActorID: mc.Mailbox.ID, Action: "mailbox_passkey_add", TargetType: "passkey", TargetID: strconv.FormatInt(passkey.ID, 10), IP: httpjson.ClientIP(r),
	})
	httpjson.Write(w, http.StatusCreated, map[string]any{"passkey": passkey})
}

func (h *Handlers) passkeyDelete(w http.ResponseWriter, r *http.Request, mc *mailCtx) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := h.Store.DeletePasskey(r.Context(), db.UserMailbox, mc.Mailbox.ID, id); err != nil {
		httpjson.Fail(w, err)
		return
	}
	_ = h.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "mailbox", ActorID: mc.Mailbox.ID, Action: "mailbox_passkey_delete", TargetType: "passkey", TargetID: strconv.FormatInt(id, 10), IP: httpjson.ClientIP(r),
	})
	httpjson.Write(w, http.StatusOK, nil)
}

func (h *Handlers) passkeyLoginOptions(w http.ResponseWriter, r *http.Request) {
	wa, _, err := auth.WebAuthnForRequest(r, branding.CurrentForHost(r.Context(), h.Store, r.Host).Name)
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
	challengeID, err := h.Store.CreateAuthChallenge(r.Context(), db.UserMailbox, 0, mailPasskeyLoginKind, string(raw), auth.PasskeyChallengeTTL)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"challenge_id": challengeID, "options": assertion.Response})
}

func (h *Handlers) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	challengeID := r.URL.Query().Get("challenge_id")
	chal, err := h.Store.GetAuthChallenge(r.Context(), challengeID, mailPasskeyLoginKind)
	if err != nil {
		httpjson.Error(w, http.StatusBadRequest, fmt.Errorf("passkey sign-in expired; try again"))
		return
	}
	var session webauthn.SessionData
	if err := json.Unmarshal([]byte(chal.SessionData), &session); err != nil {
		httpjson.Fail(w, err)
		return
	}
	wa, rpID, err := auth.WebAuthnForRequest(r, branding.CurrentForHost(r.Context(), h.Store, r.Host).Name)
	if err != nil {
		httpjson.Fail(w, err)
		return
	}
	loader := func(rawID, userHandle []byte) (webauthn.User, error) {
		p, err := h.Store.GetPasskeyByCredential(r.Context(), rpID, auth.EncodeCredentialID(rawID))
		if err != nil || p.UserType != db.UserMailbox {
			return nil, fmt.Errorf("passkey not found")
		}
		mb, err := h.Store.GetMailbox(r.Context(), p.UserID)
		if err != nil || !mb.Enabled || mb.WebAuthnHandle != auth.EncodeCredentialID(userHandle) {
			return nil, fmt.Errorf("passkey not found")
		}
		return auth.LoadWebAuthnUser(r.Context(), h.Store, h.Secret, db.UserMailbox, mb.ID, rpID, mb.Email, mb.Email)
	}
	validatedUser, validatedCredential, err := wa.FinishPasskeyLogin(loader, session, r)
	if err != nil {
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("passkey sign-in failed"))
		return
	}
	user, ok := validatedUser.(*auth.WebAuthnUser)
	if !ok || user.UserType != db.UserMailbox {
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("passkey sign-in failed"))
		return
	}
	mb, err := h.Store.GetMailbox(r.Context(), user.UserID)
	if err != nil || !mb.Enabled || mb.EncryptedPasskeyPassword == "" {
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("passkey sign-in needs one password sign-in first"))
		return
	}
	password, err := security.Decrypt(h.Secret, mb.EncryptedPasskeyPassword)
	if err != nil {
		httpjson.Error(w, http.StatusUnauthorized, fmt.Errorf("passkey sign-in needs one password sign-in first"))
		return
	}
	encrypted, err := auth.EncryptCredential(h.Secret, *validatedCredential)
	if err == nil {
		if p, perr := h.Store.GetPasskeyByCredential(r.Context(), rpID, auth.EncodeCredentialID(validatedCredential.ID)); perr == nil {
			_ = h.Store.UpdatePasskeyCredential(r.Context(), p.ID, encrypted)
		}
	}
	csrf, err := h.Sessions.Login(r.Context(), w, db.UserMailbox, mb.ID, &auth.Credentials{Email: mb.Email, Password: password})
	if err != nil {
		httpjson.Error(w, http.StatusInternalServerError, fmt.Errorf("could not create session"))
		return
	}
	_ = h.Store.DeleteAuthChallenge(r.Context(), chal.ID)
	_ = h.Store.AppendAudit(r.Context(), db.AuditLog{
		ActorType: "mailbox", ActorID: mb.ID, Action: "passkey_login", TargetType: "mailbox", TargetID: mb.Email, IP: httpjson.ClientIP(r),
	})
	httpjson.Write(w, http.StatusOK, map[string]any{"email": mb.Email, "csrf": csrf})
}
