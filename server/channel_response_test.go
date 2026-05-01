package server

import (
	"context"
	"testing"

	"github.com/jinto/kittypaw/core"
)

type plainResponseChannel struct{ sent string }

func (p *plainResponseChannel) Start(context.Context, chan<- core.Event) error { return nil }
func (p *plainResponseChannel) Name() string                                   { return "plain" }
func (p *plainResponseChannel) SendResponse(_ context.Context, _, response, _ string) error {
	p.sent = response
	return nil
}

type richResponseChannel struct {
	plainResponseChannel
	rich core.OutboundResponse
}

func (r *richResponseChannel) SendRichResponse(_ context.Context, _ string, response core.OutboundResponse, _ string) error {
	r.rich = response
	return nil
}

func TestSendChannelResponseUsesRichResponder(t *testing.T) {
	ch := &richResponseChannel{}
	out := core.OutboundResponse{Text: "fallback", Image: &core.ImageAttachment{URL: "https://cdn.example.com/cat.png"}}
	if err := sendChannelResponse(context.Background(), ch, "chat", out, "reply"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if ch.rich.Image == nil || ch.rich.Image.URL != out.Image.URL {
		t.Fatalf("rich response = %#v", ch.rich)
	}
	if ch.sent != "" {
		t.Fatalf("plain response should not be used, got %q", ch.sent)
	}
}

func TestSendChannelResponseFallsBackToText(t *testing.T) {
	ch := &plainResponseChannel{}
	out := core.OutboundResponse{Text: "fallback", Image: &core.ImageAttachment{URL: "https://cdn.example.com/cat.png"}}
	if err := sendChannelResponse(context.Background(), ch, "chat", out, "reply"); err != nil {
		t.Fatalf("send: %v", err)
	}
	if ch.sent != "fallback" {
		t.Fatalf("sent = %q", ch.sent)
	}
}
