package main

import (
	"log"
	"net/http"

	"github.com/kittypaw-app/kittychat/internal/broker"
	"github.com/kittypaw-app/kittychat/internal/config"
	"github.com/kittypaw-app/kittychat/internal/daemonws"
	"github.com/kittypaw-app/kittychat/internal/openai"
	"github.com/kittypaw-app/kittychat/internal/server"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	log.Printf("listening on %s", cfg.BindAddr)
	if err := http.ListenAndServe(cfg.BindAddr, newRouter(cfg)); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func newRouter(cfg config.Config) http.Handler {
	b := broker.New(broker.Config{})
	devicePrincipal := broker.DevicePrincipal{
		UserID:          cfg.UserID,
		DeviceID:        cfg.DeviceID,
		LocalAccountIDs: []string{cfg.LocalAccountID},
	}

	return server.NewRouter(server.Config{
		Version: cfg.Version,
		DaemonHandler: daemonws.NewHandler(daemonws.StaticTokenAuthenticator{
			Token:     cfg.DeviceToken,
			Principal: devicePrincipal,
		}, b),
		OpenAIHandler: openai.NewHandler(openai.StaticTokenAuthenticator{
			Token: cfg.APIToken,
			Principal: openai.Principal{
				UserID:    cfg.UserID,
				DeviceID:  cfg.DeviceID,
				AccountID: cfg.LocalAccountID,
			},
		}, b),
	})
}
