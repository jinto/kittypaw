package store

import (
	"context"
	"testing"
	"time"

	"github.com/kittypaw-app/kittykakao/internal/relay"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestTokenRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	exists, err := s.TokenExists(ctx, "abc")
	if err != nil {
		t.Fatalf("token exists: %v", err)
	}
	if exists {
		t.Fatal("token exists before insert")
	}

	if err := s.PutToken(ctx, "abc"); err != nil {
		t.Fatalf("put token: %v", err)
	}
	exists, err = s.TokenExists(ctx, "abc")
	if err != nil {
		t.Fatalf("token exists after insert: %v", err)
	}
	if !exists {
		t.Fatal("token missing after insert")
	}
}

func TestUserMappingCRUD(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	_, ok, err := s.GetUserMapping(ctx, "kakao1")
	if err != nil {
		t.Fatalf("get missing mapping: %v", err)
	}
	if ok {
		t.Fatal("mapping exists before insert")
	}

	if err := s.PutUserMapping(ctx, "kakao1", "tok1"); err != nil {
		t.Fatalf("put mapping: %v", err)
	}
	token, ok, err := s.GetUserMapping(ctx, "kakao1")
	if err != nil {
		t.Fatalf("get mapping: %v", err)
	}
	if !ok || token != "tok1" {
		t.Fatalf("mapping = %q %v, want tok1 true", token, ok)
	}

	if err := s.DeleteUserMapping(ctx, "kakao1"); err != nil {
		t.Fatalf("delete mapping: %v", err)
	}
	_, ok, err = s.GetUserMapping(ctx, "kakao1")
	if err != nil {
		t.Fatalf("get deleted mapping: %v", err)
	}
	if ok {
		t.Fatal("mapping exists after delete")
	}
}

func TestKillswitchToggle(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	enabled, err := s.GetKillswitch(ctx)
	if err != nil {
		t.Fatalf("get default killswitch: %v", err)
	}
	if enabled {
		t.Fatal("killswitch enabled by default")
	}
	if err := s.SetKillswitch(ctx, true); err != nil {
		t.Fatalf("enable killswitch: %v", err)
	}
	enabled, err = s.GetKillswitch(ctx)
	if err != nil {
		t.Fatalf("get enabled killswitch: %v", err)
	}
	if !enabled {
		t.Fatal("killswitch not enabled")
	}
	if err := s.SetKillswitch(ctx, false); err != nil {
		t.Fatalf("disable killswitch: %v", err)
	}
	enabled, err = s.GetKillswitch(ctx)
	if err != nil {
		t.Fatalf("get disabled killswitch: %v", err)
	}
	if enabled {
		t.Fatal("killswitch still enabled")
	}
}

func TestPendingPutTakeAtomic(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	pending := relay.PendingContext{
		CallbackURL: "https://cb.kakao.com/x",
		UserID:      "user1",
		CreatedAt:   time.Now().Unix(),
	}

	if err := s.PutPending(ctx, "act1", pending); err != nil {
		t.Fatalf("put pending: %v", err)
	}
	taken, ok, err := s.TakePending(ctx, "act1")
	if err != nil {
		t.Fatalf("take pending: %v", err)
	}
	if !ok {
		t.Fatal("first take returned missing")
	}
	if taken.CallbackURL != pending.CallbackURL {
		t.Fatalf("CallbackURL = %q", taken.CallbackURL)
	}

	_, ok, err = s.TakePending(ctx, "act1")
	if err != nil {
		t.Fatalf("second take pending: %v", err)
	}
	if ok {
		t.Fatal("second take returned pending entry")
	}
}

func TestRateLimitIncrementsAndCaps(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	for want := uint64(1); want <= 3; want++ {
		got, err := s.CheckRateLimit(ctx, 3, 100)
		if err != nil {
			t.Fatalf("check rate limit %d: %v", want, err)
		}
		if !got.OK || got.Daily != want {
			t.Fatalf("rate result = %+v, want ok daily=%d", got, want)
		}
	}

	got, err := s.CheckRateLimit(ctx, 3, 100)
	if err != nil {
		t.Fatalf("check capped rate limit: %v", err)
	}
	if got.OK || got.Daily != 3 {
		t.Fatalf("capped result = %+v, want not ok daily=3", got)
	}
}

func TestStatsMatchAfterIncrements(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)

	if _, err := s.CheckRateLimit(ctx, 100, 100); err != nil {
		t.Fatalf("increment 1: %v", err)
	}
	if _, err := s.CheckRateLimit(ctx, 100, 100); err != nil {
		t.Fatalf("increment 2: %v", err)
	}

	stats, err := s.GetStats(ctx)
	if err != nil {
		t.Fatalf("get stats: %v", err)
	}
	if stats.Daily != 2 || stats.Monthly != 2 {
		t.Fatalf("stats = %+v, want daily/monthly 2", stats)
	}
}

func TestCleanupExpiredPendingRemovesOld(t *testing.T) {
	ctx := context.Background()
	s := testStore(t)
	old := relay.PendingContext{
		CallbackURL: "https://cb.kakao.com/old",
		UserID:      "u1",
		CreatedAt:   time.Now().Unix() - 700,
	}
	fresh := relay.PendingContext{
		CallbackURL: "https://cb.kakao.com/new",
		UserID:      "u2",
		CreatedAt:   time.Now().Unix(),
	}

	if err := s.PutPending(ctx, "old_act", old); err != nil {
		t.Fatalf("put old pending: %v", err)
	}
	if err := s.PutPending(ctx, "new_act", fresh); err != nil {
		t.Fatalf("put fresh pending: %v", err)
	}

	deleted, err := s.CleanupExpiredPending(ctx, 600)
	if err != nil {
		t.Fatalf("cleanup pending: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted = %d, want 1", deleted)
	}

	if _, ok, err := s.TakePending(ctx, "old_act"); err != nil || ok {
		t.Fatalf("old pending after cleanup ok=%v err=%v", ok, err)
	}
	if _, ok, err := s.TakePending(ctx, "new_act"); err != nil || !ok {
		t.Fatalf("fresh pending after cleanup ok=%v err=%v", ok, err)
	}
}
