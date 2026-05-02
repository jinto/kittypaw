package janitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kittypaw-app/kittyportal/internal/model"
)

// fakeDeviceStore captures Tick's calls into ReapIdle and
// DeleteRevokedOlderThan. The Create/FindByID/etc. methods are
// stubbed because Tick never reaches them — adding panics catches
// any regression that accidentally widens janitor's contract.
type fakeDeviceStore struct {
	reapCalledWith      time.Time
	reapErr             error
	reapCount           int64
	deleteRevokedCalled time.Time
	deleteRevokedErr    error
	deleteRevokedCount  int64
}

func (f *fakeDeviceStore) Create(_ context.Context, _, _ string, _ map[string]any) (*model.Device, error) {
	panic("Tick must not call Create")
}
func (f *fakeDeviceStore) FindByID(_ context.Context, _ string) (*model.Device, error) {
	panic("Tick must not call FindByID")
}
func (f *fakeDeviceStore) ListActiveForUser(_ context.Context, _ string) ([]*model.Device, error) {
	panic("Tick must not call ListActiveForUser")
}
func (f *fakeDeviceStore) Revoke(_ context.Context, _ string) error {
	panic("Tick must not call Revoke")
}
func (f *fakeDeviceStore) Touch(_ context.Context, _ string) error {
	panic("Tick must not call Touch")
}
func (f *fakeDeviceStore) ReapIdle(_ context.Context, olderThan time.Time) (int64, error) {
	f.reapCalledWith = olderThan
	return f.reapCount, f.reapErr
}
func (f *fakeDeviceStore) DeleteRevokedOlderThan(_ context.Context, olderThan time.Time) (int64, error) {
	f.deleteRevokedCalled = olderThan
	return f.deleteRevokedCount, f.deleteRevokedErr
}

type fakeRefreshStore struct {
	deleteExpiredCalled time.Time
	deleteExpiredErr    error
	deleteExpiredCount  int64
}

func (f *fakeRefreshStore) Create(_ context.Context, _, _ string, _ time.Time) error {
	panic("Tick must not call Create")
}
func (f *fakeRefreshStore) CreateForDevice(_ context.Context, _, _, _ string, _ time.Time) error {
	panic("Tick must not call CreateForDevice")
}
func (f *fakeRefreshStore) FindByHash(_ context.Context, _ string) (*model.RefreshToken, error) {
	panic("Tick must not call FindByHash")
}
func (f *fakeRefreshStore) RevokeIfActive(_ context.Context, _ string) (bool, error) {
	panic("Tick must not call RevokeIfActive")
}
func (f *fakeRefreshStore) RevokeAllForUser(_ context.Context, _ string) error {
	panic("Tick must not call RevokeAllForUser")
}
func (f *fakeRefreshStore) RevokeAllForDevice(_ context.Context, _ string) error {
	panic("Tick must not call RevokeAllForDevice")
}
func (f *fakeRefreshStore) RotateForDevice(_ context.Context, _, _, _, _ string, _ time.Time) error {
	panic("Tick must not call RotateForDevice")
}
func (f *fakeRefreshStore) DeleteExpiredOlderThan(_ context.Context, olderThan time.Time) (int64, error) {
	f.deleteExpiredCalled = olderThan
	return f.deleteExpiredCount, f.deleteExpiredErr
}

// frozenClock returns the same time on every Now() call. After is
// intentionally not exercised by Tick tests.
type frozenClock struct{ t time.Time }

func (c frozenClock) Now() time.Time                         { return c.t }
func (c frozenClock) After(_ time.Duration) <-chan time.Time { return nil }

// TestTick_CutoffsMatchPolicy pins the contract that Tick subtracts
// each policy duration from clock.Now() to compute its cutoff. A
// regression that, say, forgot to negate the duration (or used the
// wrong policy field) would silently delete the wrong rows in prod —
// 30-day expired refresh becoming 90-day idle threshold means a fresh
// device gets reaped on day 31.
func TestTick_CutoffsMatchPolicy(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	policy := Policy{
		IdleThreshold:           60 * 24 * time.Hour,
		RevokedRetention:        90 * 24 * time.Hour,
		ExpiredRefreshRetention: 30 * 24 * time.Hour,
	}
	devs := &fakeDeviceStore{}
	refresh := &fakeRefreshStore{}
	j := New(devs, refresh, policy, frozenClock{t: now})

	j.Tick(context.Background())

	if got, want := refresh.deleteExpiredCalled, now.Add(-policy.ExpiredRefreshRetention); !got.Equal(want) {
		t.Errorf("DeleteExpiredOlderThan cutoff = %v, want %v", got, want)
	}
	if got, want := devs.reapCalledWith, now.Add(-policy.IdleThreshold); !got.Equal(want) {
		t.Errorf("ReapIdle cutoff = %v, want %v", got, want)
	}
	if got, want := devs.deleteRevokedCalled, now.Add(-policy.RevokedRetention); !got.Equal(want) {
		t.Errorf("DeleteRevokedOlderThan cutoff = %v, want %v", got, want)
	}
}

// TestTick_ContinuesPastErrors pins the no-short-circuit contract:
// a transient DB error in step 1 (DeleteExpired) must not skip
// step 2 (ReapIdle) or step 3 (DeleteRevoked). Without this, a
// single bad day of pgx errors would leave revoked rows accumulating
// indefinitely.
func TestTick_ContinuesPastErrors(t *testing.T) {
	now := time.Now()
	devs := &fakeDeviceStore{
		reapErr:          errors.New("transient pgx error"),
		deleteRevokedErr: nil,
	}
	refresh := &fakeRefreshStore{
		deleteExpiredErr: errors.New("transient pgx error"),
	}
	j := New(devs, refresh, DefaultPolicy, frozenClock{t: now})

	j.Tick(context.Background())

	// All three should have been called even though two errored.
	if devs.reapCalledWith.IsZero() {
		t.Error("ReapIdle was not called after DeleteExpiredOlderThan errored")
	}
	if devs.deleteRevokedCalled.IsZero() {
		t.Error("DeleteRevokedOlderThan was not called after upstream errored")
	}
	if refresh.deleteExpiredCalled.IsZero() {
		t.Error("DeleteExpiredOlderThan was not called")
	}
}

// TestNextRunAt covers the only non-trivial bit of the timer logic:
// whether the same-day target has passed yet. Time-zone bugs here
// (UTC vs KST) would make the janitor fire 9 hours late, which is
// "no failure but wrong window" — the worst kind of silent bug.
func TestNextRunAt(t *testing.T) {
	kst := time.FixedZone("KST", 9*3600)
	cases := []struct {
		name string
		now  time.Time
		hour int
		want time.Time
	}{
		{
			name: "before today's run — same day",
			now:  time.Date(2026, 5, 1, 3, 30, 0, 0, kst),
			hour: 4,
			want: time.Date(2026, 5, 1, 4, 0, 0, 0, kst),
		},
		{
			name: "after today's run — next day",
			now:  time.Date(2026, 5, 1, 4, 30, 0, 0, kst),
			hour: 4,
			want: time.Date(2026, 5, 2, 4, 0, 0, 0, kst),
		},
		{
			name: "exactly at run time — next day (strict After)",
			now:  time.Date(2026, 5, 1, 4, 0, 0, 0, kst),
			hour: 4,
			want: time.Date(2026, 5, 2, 4, 0, 0, 0, kst),
		},
		{
			name: "UTC input crossing into next KST day",
			// 2026-05-01 20:00 UTC = 2026-05-02 05:00 KST → past 04:00 → next day
			now:  time.Date(2026, 5, 1, 20, 0, 0, 0, time.UTC),
			hour: 4,
			want: time.Date(2026, 5, 3, 4, 0, 0, 0, kst),
		},
		{
			name: "UTC input before today's KST run",
			// 2026-05-01 18:00 UTC = 2026-05-02 03:00 KST → before 04:00 → same KST day
			now:  time.Date(2026, 5, 1, 18, 0, 0, 0, time.UTC),
			hour: 4,
			want: time.Date(2026, 5, 2, 4, 0, 0, 0, kst),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nextRunAt(tc.now, tc.hour)
			if !got.Equal(tc.want) {
				t.Errorf("nextRunAt(%v, %d) = %v, want %v", tc.now, tc.hour, got, tc.want)
			}
		})
	}
}

// TestRun_ContextCancelStops pins the graceful-shutdown contract: a
// canceled ctx must return Run before the next clock.After fires.
// Otherwise main.go's shutdown would hang on the janitor goroutine
// for up to 24 hours.
func TestRun_ContextCancelStops(t *testing.T) {
	devs := &fakeDeviceStore{}
	refresh := &fakeRefreshStore{}
	// after channel that never fires — only ctx.Done() can unblock Run.
	never := make(chan time.Time)
	c := stuckClock{now: time.Now(), after: never}
	j := New(devs, refresh, DefaultPolicy, c)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		j.Run(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// expected
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancel")
	}
}

type stuckClock struct {
	now   time.Time
	after <-chan time.Time
}

func (c stuckClock) Now() time.Time                         { return c.now }
func (c stuckClock) After(_ time.Duration) <-chan time.Time { return c.after }
