package main

import (
	"net"
	"strings"
	"testing"
)

func TestPreflightPortSuggestsRemoteFlag(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	port := ln.Addr().(*net.TCPAddr).Port
	err = preflightPort("127.0.0.1", port)
	if err == nil {
		t.Fatal("preflightPort succeeded on an occupied port")
	}
	msg := err.Error()
	if !strings.Contains(msg, "kittypaw chat --remote http://127.0.0.1:3001") {
		t.Fatalf("port collision message does not suggest --remote: %s", msg)
	}
	if strings.Contains(msg, "chat --server") {
		t.Fatalf("port collision message suggests removed --server flag: %s", msg)
	}
}
