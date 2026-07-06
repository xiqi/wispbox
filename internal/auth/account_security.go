package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
	"github.com/xiqi/wispbox/internal/db"
	"github.com/xiqi/wispbox/internal/security"
)

const (
	TOTPIssuerFallback  = "wispbox"
	PasskeyChallengeTTL = 5 * time.Minute
)

func GenerateTOTPSecret(issuer, account string) (secret, uri string, err error) {
	issuer = strings.TrimSpace(issuer)
	if issuer == "" {
		issuer = TOTPIssuerFallback
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      issuer,
		AccountName: account,
		Period:      30,
		SecretSize:  20,
		Digits:      otp.DigitsSix,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		return "", "", err
	}
	return key.Secret(), key.URL(), nil
}

func ValidateTOTP(secret, code string) bool {
	code = strings.TrimSpace(strings.ReplaceAll(code, " ", ""))
	if code == "" || secret == "" {
		return false
	}
	return totp.Validate(code, secret)
}

type WebAuthnUser struct {
	ID          []byte
	Name        string
	DisplayName string
	Credentials []webauthn.Credential
	UserType    db.UserType
	UserID      int64
}

func (u WebAuthnUser) WebAuthnID() []byte                         { return u.ID }
func (u WebAuthnUser) WebAuthnName() string                       { return u.Name }
func (u WebAuthnUser) WebAuthnDisplayName() string                { return u.DisplayName }
func (u WebAuthnUser) WebAuthnCredentials() []webauthn.Credential { return u.Credentials }

func DecodeWebAuthnHandle(handle string) ([]byte, error) {
	raw, err := base64.RawURLEncoding.DecodeString(handle)
	if err != nil || len(raw) == 0 || len(raw) > 64 {
		return nil, fmt.Errorf("invalid passkey user handle")
	}
	return raw, nil
}

func EncodeCredentialID(id []byte) string {
	return base64.RawURLEncoding.EncodeToString(id)
}

func EncryptCredential(secret []byte, cred webauthn.Credential) (string, error) {
	raw, err := json.Marshal(cred)
	if err != nil {
		return "", err
	}
	return security.Encrypt(secret, string(raw))
}

func DecryptCredential(secret []byte, encrypted string) (webauthn.Credential, error) {
	var cred webauthn.Credential
	raw, err := security.Decrypt(secret, encrypted)
	if err != nil {
		return cred, err
	}
	if err := json.Unmarshal([]byte(raw), &cred); err != nil {
		return cred, err
	}
	return cred, nil
}

func WebAuthnForRequest(r *http.Request, displayName string) (*webauthn.WebAuthn, string, error) {
	rpID := requestRPID(r)
	if rpID == "" {
		return nil, "", fmt.Errorf("passkeys need a valid host")
	}
	displayName = strings.TrimSpace(displayName)
	if displayName == "" {
		displayName = TOTPIssuerFallback
	}
	origin := requestOrigin(r)
	wa, err := webauthn.New(&webauthn.Config{
		RPDisplayName:         displayName,
		RPID:                  rpID,
		RPOrigins:             []string{origin},
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
	})
	return wa, rpID, err
}

func requestOrigin(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	} else if xf := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); xf == "https" || xf == "http" {
		scheme = xf
	}
	return scheme + "://" + r.Host
}

func requestRPID(r *http.Request) string {
	host := r.Host
	if xf := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); xf != "" {
		host = xf
	}
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return strings.ToLower(host)
}

func LoadWebAuthnUser(ctx context.Context, store *db.Store, secret []byte, userType db.UserType, userID int64, rpID, name, displayName string) (*WebAuthnUser, error) {
	handle, err := store.EnsureWebAuthnHandle(ctx, userType, userID)
	if err != nil {
		return nil, err
	}
	rawHandle, err := DecodeWebAuthnHandle(handle)
	if err != nil {
		return nil, err
	}
	passkeys, err := store.ListPasskeys(ctx, userType, userID, rpID)
	if err != nil {
		return nil, err
	}
	creds := make([]webauthn.Credential, 0, len(passkeys))
	for _, p := range passkeys {
		cred, err := DecryptCredential(secret, p.EncryptedCredential)
		if err != nil {
			return nil, err
		}
		creds = append(creds, cred)
	}
	return &WebAuthnUser{
		ID: rawHandle, Name: name, DisplayName: displayName,
		Credentials: creds, UserType: userType, UserID: userID,
	}, nil
}
