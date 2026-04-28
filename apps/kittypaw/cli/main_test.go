package main

import (
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/jinto/kittypaw/client"
)

func TestIsTransportDropErr_StringMatches(t *testing.T) {
	cases := []string{
		"EOF",
		"unexpected EOF",
		"write tcp 127.0.0.1:57428->127.0.0.1:3000: write: broken pipe",
		"failed to flush: write: broken pipe",
		"use of closed network connection",
		"read: connection reset by peer",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if !isTransportDropErr(errors.New(msg)) {
				t.Errorf("expected transport-drop classification for %q", msg)
			}
		})
	}
}

func TestIsTransportDropErr_TypedSentinels(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"io.EOF", io.EOF},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF},
		{"syscall.EPIPE", syscall.EPIPE},
		{"syscall.ECONNRESET", syscall.ECONNRESET},
		{"net.ErrClosed", net.ErrClosed},
		{"wrapped io.EOF", fmt.Errorf("read ws msg: %w", io.EOF)},
		{"wrapped EPIPE", fmt.Errorf("write chat msg: %w", syscall.EPIPE)},
		{"deeply wrapped ECONNRESET", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", syscall.ECONNRESET))},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !isTransportDropErr(tc.err) {
				t.Errorf("expected transport-drop for %v", tc.err)
			}
		})
	}
}

func TestIsTransportDropErr_RejectsServerSide(t *testing.T) {
	// Server errors carry ErrServerSide via client/ws.go. Even when the
	// embedded message contains "EOF" / "broken pipe" / etc. the silent-
	// reconnect path must not retry — replaying would double-charge the
	// user without healing the underlying server failure.
	cases := []error{
		fmt.Errorf("%w: decode response: unexpected EOF", client.ErrServerSide),
		fmt.Errorf("%w: upstream returned broken pipe", client.ErrServerSide),
		fmt.Errorf("%w: connection reset by peer in tool result", client.ErrServerSide),
		fmt.Errorf("%w: use of closed network connection from skill", client.ErrServerSide),
		client.ErrServerSide,
	}
	for _, err := range cases {
		t.Run(err.Error(), func(t *testing.T) {
			if isTransportDropErr(err) {
				t.Errorf("server-side error %q must NOT classify as transport drop", err)
			}
		})
	}
}

func TestIsTransportDropErr_NegativeCases(t *testing.T) {
	cases := []string{
		"daemon failed to start",
		"chat protocol invalid",
		"unauthorized: 401",
		"timeout exceeded",
	}
	for _, msg := range cases {
		t.Run(msg, func(t *testing.T) {
			if isTransportDropErr(errors.New(msg)) {
				t.Errorf("non-transport error %q must not classify as drop", msg)
			}
		})
	}
}

func TestIsTransportDropErr_NilSafe(t *testing.T) {
	if isTransportDropErr(nil) {
		t.Fatal("nil error must not classify as transport drop")
	}
}
