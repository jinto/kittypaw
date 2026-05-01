package identity

import (
	"context"
	"fmt"
	"slices"

	"github.com/golang-jwt/jwt/v5"
)

type JWTVerifierConfig struct {
	Secret           string
	DefaultDeviceID  string
	DefaultAccountID string
}

type JWTCredentialVerifier struct {
	secret           []byte
	defaultDeviceID  string
	defaultAccountID string
}

type jwtCredentialClaims struct {
	Scope           []string `json:"scope,omitempty"`
	Version         int      `json:"v,omitempty"`
	UserID          string   `json:"user_id,omitempty"`
	DeviceID        string   `json:"device_id,omitempty"`
	AccountID       string   `json:"account_id,omitempty"`
	LocalAccountIDs []string `json:"local_accounts,omitempty"`
	jwt.RegisteredClaims
}

func NewJWTCredentialVerifier(cfg JWTVerifierConfig) (*JWTCredentialVerifier, error) {
	if cfg.Secret == "" {
		return nil, fmt.Errorf("jwt secret is required")
	}
	return &JWTCredentialVerifier{
		secret:           []byte(cfg.Secret),
		defaultDeviceID:  cfg.DefaultDeviceID,
		defaultAccountID: cfg.DefaultAccountID,
	}, nil
}

func (v *JWTCredentialVerifier) VerifyAPIClient(ctx context.Context, token string) (APIClientClaims, error) {
	if err := ctx.Err(); err != nil {
		return APIClientClaims{}, err
	}
	claims, err := v.parse(token)
	if err != nil {
		return APIClientClaims{}, err
	}

	scopes := toScopes(claims.Scope)
	apiClaims := APIClientClaims{
		Subject:   claims.Subject,
		Audiences: []string(claims.Audience),
		Version:   claims.Version,
		Scopes:    scopes,
		UserID:    claims.Subject,
		DeviceID:  firstNonEmpty(claims.DeviceID, v.defaultDeviceID),
		AccountID: firstNonEmpty(claims.AccountID, v.defaultAccountID),
	}
	if err := validateAPIClientClaims(apiClaims); err != nil {
		return APIClientClaims{}, ErrUnauthorized
	}
	if !hasScope(scopes, ScopeChatRelay) && !hasScope(scopes, ScopeModelsRead) {
		return APIClientClaims{}, ErrUnauthorized
	}
	return apiClaims, nil
}

func (v *JWTCredentialVerifier) VerifyDevice(ctx context.Context, token string) (DeviceClaims, error) {
	if err := ctx.Err(); err != nil {
		return DeviceClaims{}, err
	}
	claims, err := v.parse(token)
	if err != nil {
		return DeviceClaims{}, err
	}

	scopes := toScopes(claims.Scope)
	deviceClaims := DeviceClaims{
		Subject:         claims.Subject,
		Audiences:       []string(claims.Audience),
		Version:         claims.Version,
		Scopes:          scopes,
		UserID:          claims.UserID,
		DeviceID:        claims.DeviceID,
		LocalAccountIDs: append([]string(nil), claims.LocalAccountIDs...),
	}
	if err := validateDeviceClaims(deviceClaims); err != nil {
		return DeviceClaims{}, ErrUnauthorized
	}
	if !hasScope(scopes, ScopeDaemonConnect) {
		return DeviceClaims{}, ErrUnauthorized
	}
	return deviceClaims, nil
}

func (v *JWTCredentialVerifier) parse(tokenString string) (*jwtCredentialClaims, error) {
	if tokenString == "" {
		return nil, ErrUnauthorized
	}
	claims := &jwtCredentialClaims{}
	token, err := jwt.ParseWithClaims(
		tokenString,
		claims,
		func(token *jwt.Token) (any, error) {
			if token.Method != jwt.SigningMethodHS256 {
				return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
			}
			return v.secret, nil
		},
		jwt.WithIssuer(IssuerKittyAPI),
		jwt.WithAudience(AudienceKittyChat),
	)
	if err != nil {
		return nil, ErrUnauthorized
	}
	if !token.Valid || claims.ExpiresAt == nil || claims.Subject == "" || claims.Version != CredentialVersion1 {
		return nil, ErrUnauthorized
	}
	return claims, nil
}

func toScopes(raw []string) []Scope {
	scopes := make([]Scope, 0, len(raw))
	for _, scope := range raw {
		typed := Scope(scope)
		if knownScope(typed) {
			scopes = append(scopes, typed)
		}
	}
	return scopes
}

func hasScope(scopes []Scope, want Scope) bool {
	return slices.Contains(scopes, want)
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
