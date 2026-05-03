package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/jinto/kittypaw/core"
)

var errKakaoRelayUnavailable = errors.New("kakao relay URL unavailable")

type kakaoPairingDeps struct {
	fetchDiscovery       func(string) (*core.DiscoveryResponse, error)
	registerRelaySession func(string) (*core.RelayRegistration, error)
	copyToClipboard      func(string) error
	openBrowser          func(string) error
	checkRelayPairStatus func(string, string) bool
	after                func(time.Duration) <-chan time.Time
}

type preparedKakaoPairing struct {
	Wizard       core.WizardResult
	RelayBase    string
	Registration *core.RelayRegistration
}

var runKakaoPairingWizard = func(stdout io.Writer) (core.WizardResult, error) {
	return runKakaoPairingWizardWithDeps(stdout, core.DefaultAPIServerURL, defaultKakaoPairingDeps())
}

func defaultKakaoPairingDeps() kakaoPairingDeps {
	return kakaoPairingDeps{
		fetchDiscovery:       core.FetchDiscovery,
		registerRelaySession: core.RegisterRelaySession,
		copyToClipboard:      copyToClipboard,
		openBrowser:          core.OpenBrowser,
		checkRelayPairStatus: core.CheckRelayPairStatus,
		after:                time.After,
	}
}

func runKakaoPairingWizardWithDeps(stdout io.Writer, apiURL string, deps kakaoPairingDeps) (core.WizardResult, error) {
	prepared, err := prepareKakaoPairing(apiURL, deps)
	if err != nil {
		return core.WizardResult{}, err
	}
	presentKakaoPairing(stdout, prepared, deps)
	return prepared.Wizard, nil
}

func prepareKakaoPairing(apiURL string, deps kakaoPairingDeps) (preparedKakaoPairing, error) {
	deps = fillKakaoPairingDeps(deps)
	d, err := deps.fetchDiscovery(apiURL)
	if err != nil {
		return preparedKakaoPairing{}, fmt.Errorf("fetch kakao discovery: %w", err)
	}
	if d.KakaoRelayURL == "" {
		return preparedKakaoPairing{}, errKakaoRelayUnavailable
	}
	reg, err := deps.registerRelaySession(d.KakaoRelayURL)
	if err != nil {
		return preparedKakaoPairing{}, fmt.Errorf("register kakao relay: %w", err)
	}
	wsURL := core.WSURLFromRelay(d.KakaoRelayURL, reg.Token)
	return preparedKakaoPairing{
		Wizard: core.WizardResult{
			APIServerURL:    apiURL,
			KakaoEnabled:    true,
			KakaoRelayWSURL: wsURL,
		},
		RelayBase:    d.KakaoRelayURL,
		Registration: reg,
	}, nil
}

func fillKakaoPairingDeps(deps kakaoPairingDeps) kakaoPairingDeps {
	defaults := defaultKakaoPairingDeps()
	if deps.fetchDiscovery == nil {
		deps.fetchDiscovery = defaults.fetchDiscovery
	}
	if deps.registerRelaySession == nil {
		deps.registerRelaySession = defaults.registerRelaySession
	}
	if deps.copyToClipboard == nil {
		deps.copyToClipboard = defaults.copyToClipboard
	}
	if deps.openBrowser == nil {
		deps.openBrowser = defaults.openBrowser
	}
	if deps.checkRelayPairStatus == nil {
		deps.checkRelayPairStatus = defaults.checkRelayPairStatus
	}
	if deps.after == nil {
		deps.after = defaults.after
	}
	return deps
}

func presentKakaoPairing(stdout io.Writer, prepared preparedKakaoPairing, deps kakaoPairingDeps) {
	if stdout == nil {
		stdout = io.Discard
	}
	deps = fillKakaoPairingDeps(deps)
	reg := prepared.Registration

	_, _ = fmt.Fprintln(stdout)
	if err := deps.copyToClipboard(reg.PairCode); err == nil {
		_, _ = fmt.Fprintf(stdout, "  인증코드 %s 이 클립보드에 복사되었습니다.\n", reg.PairCode)
	} else {
		_, _ = fmt.Fprintf(stdout, "  인증코드: %s\n", reg.PairCode)
	}

	_, _ = fmt.Fprintf(stdout, "  인증코드 %s 을 채널에 전송하세요.\n", reg.PairCode)
	_, _ = fmt.Fprintln(stdout)
	if err := deps.openBrowser(reg.ChannelURL); err != nil {
		_, _ = fmt.Fprintf(stdout, "  채널 URL: %s\n", reg.ChannelURL)
	}

	_, _ = fmt.Fprint(stdout, "  페어링 대기 중")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			_, _ = fmt.Fprintln(stdout, " 시간 초과")
			_, _ = fmt.Fprintln(stdout, "  ✓ KakaoTalk 활성화 완료 (나중에 채널에서 인증코드를 전송하세요)")
			return
		case <-deps.after(3 * time.Second):
			_, _ = fmt.Fprint(stdout, ".")
			if deps.checkRelayPairStatus(prepared.RelayBase, reg.Token) {
				_, _ = fmt.Fprintln(stdout, " OK")
				_, _ = fmt.Fprintln(stdout, "  ✓ KakaoTalk 페어링 완료!")
				return
			}
		}
	}
}
