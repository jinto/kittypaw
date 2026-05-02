package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/config"
	"github.com/kittypaw-app/kittychat/internal/daemonws"
	"github.com/kittypaw-app/kittychat/internal/identity"
	"github.com/kittypaw-app/kittychat/internal/openai"
	"github.com/kittypaw-app/kittychat/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	router, err := newRouter(cfg)
	if err != nil {
		log.Fatalf("router: %v", err)
	}

	log.Printf("listening on %s", cfg.BindAddr)
	if err := http.ListenAndServe(cfg.BindAddr, router); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func newRouter(cfg config.Config) (http.Handler, error) {
	verifier, err := newCredentialVerifier(cfg)
	if err != nil {
		return nil, err
	}
	b := broker.New(broker.Config{})

	return server.NewRouter(server.Config{
		Version: cfg.Version,
		DaemonHandler: daemonws.NewHandler(identity.DeviceAuthenticator{
			Verifier: verifier,
		}, b),
		OpenAIHandler: openai.NewHandler(identity.APIAuthenticator{
			Verifier: verifier,
		}, b),
	}), nil
}

func newCredentialVerifier(cfg config.Config) (identity.CredentialVerifier, error) {
	verifier := identity.NewMemoryCredentialVerifier()
	if cfg.APIToken == "" && cfg.JWTSecret == "" && cfg.JWKSURL == "" {
		return nil, fmt.Errorf("api token, jwt secret, or jwks url is required")
	}
	hasStaticSeed := false
	if cfg.APIToken != "" {
		if err := verifier.AddAPIClient(cfg.APIToken, identity.APIClientClaims{
			Subject:   cfg.UserID,
			Audiences: []string{identity.AudienceKittyChat},
			Version:   identity.CredentialVersion1,
			Scopes:    []identity.Scope{identity.ScopeChatRelay, identity.ScopeModelsRead},
			UserID:    cfg.UserID,
			DeviceID:  cfg.DeviceID,
			AccountID: cfg.LocalAccountID,
		}); err != nil {
			return nil, fmt.Errorf("seed api client: %w", err)
		}
		hasStaticSeed = true
	}
	if cfg.DeviceToken != "" {
		if err := verifier.AddDevice(cfg.DeviceToken, identity.DeviceClaims{
			Subject:         "device:" + cfg.DeviceID,
			Audiences:       []string{identity.AudienceKittyChat},
			Version:         identity.CredentialVersion1,
			Scopes:          []identity.Scope{identity.ScopeDaemonConnect},
			UserID:          cfg.UserID,
			DeviceID:        cfg.DeviceID,
			LocalAccountIDs: []string{cfg.LocalAccountID},
		}); err != nil {
			return nil, fmt.Errorf("seed device: %w", err)
		}
		hasStaticSeed = true
	}
	if cfg.JWTSecret != "" || cfg.JWKSURL != "" {
		jwtVerifier, err := identity.NewJWTCredentialVerifier(identity.JWTVerifierConfig{
			Secret:  cfg.JWTSecret,
			JWKSURL: cfg.JWKSURL,
		})
		if err != nil {
			return nil, fmt.Errorf("jwt verifier: %w", err)
		}
		if hasStaticSeed {
			return identity.ChainCredentialVerifier{
				Verifiers: []identity.CredentialVerifier{jwtVerifier, verifier},
			}, nil
		}
		return jwtVerifier, nil
	}
	return verifier, nil
}
