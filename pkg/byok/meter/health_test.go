package meter_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"

	"github.com/go-go-golems/llm-proxy/pkg/byok/authmw"
	"github.com/go-go-golems/llm-proxy/pkg/byok/meter"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
	"github.com/go-go-golems/llm-proxy/pkg/byok/store/memory"
)

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type controlledStore struct {
	store.Store
	mu          sync.Mutex
	recordErr   error
	probeErr    error
	recordCalls int
	probeCalls  int
	probeEnter  chan struct{}
	probeBlock  chan struct{}
}

func (s *controlledStore) RecordUsage(ctx context.Context, entry store.LedgerEntry) error {
	s.mu.Lock()
	s.recordCalls++
	err := s.recordErr
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.Store.RecordUsage(ctx, entry)
}

func (s *controlledStore) CheckMeteringHealth(ctx context.Context) error {
	s.mu.Lock()
	s.probeCalls++
	err := s.probeErr
	enter, block := s.probeEnter, s.probeBlock
	s.mu.Unlock()
	if enter != nil {
		enter <- struct{}{}
	}
	if block != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-block:
		}
	}
	return err
}

func (s *controlledStore) setProbeError(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.probeErr = err
}

func TestPersistentFailureOpensAndCommittedProbeRecovers(t *testing.T) {
	ctx := context.Background()
	base := memory.New()
	controlled := &controlledStore{Store: base}
	clock := &fakeClock{now: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)}
	health, err := meter.NewHealth(controlled, base, meter.HealthConfig{
		TransientFailureThreshold: 3,
		RecoveryCooldown:          time.Minute,
		Now:                       clock.Now,
	})
	if err != nil {
		t.Fatalf("new health: %v", err)
	}

	health.RecordFailure(ctx, errors.New("durable write failed"))
	if got := health.Snapshot(); got.State != meter.CircuitOpen || got.PersistentFailures != 1 || got.OpenedTotal != 1 {
		t.Fatalf("opened snapshot = %+v", got)
	}
	if health.Ready(ctx) == nil {
		t.Fatal("open circuit reported ready")
	}
	if err := health.BeforeInference(ctx); !errors.Is(err, meter.ErrUnavailable) {
		t.Fatalf("before cooldown = %v", err)
	}
	if controlled.probeCalls != 0 {
		t.Fatalf("probe ran before cooldown: %d", controlled.probeCalls)
	}

	clock.Advance(time.Minute)
	if err := health.BeforeInference(ctx); err != nil {
		t.Fatalf("recovery probe rejected healthy store: %v", err)
	}
	if got := health.Snapshot(); got.State != meter.CircuitClosed || got.RecoveredTotal != 1 || got.ConsecutiveTransient != 0 {
		t.Fatalf("recovered snapshot = %+v", got)
	}
	if controlled.probeCalls != 1 {
		t.Fatalf("probe calls = %d, want 1", controlled.probeCalls)
	}
	events, err := base.ListEvents(ctx, store.AuditFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list circuit audit: %v", err)
	}
	seen := map[string]bool{}
	for _, event := range events {
		seen[event.EventType] = true
	}
	if !seen[store.AuditMeterCircuitOpened] || !seen[store.AuditMeterCircuitClosed] {
		t.Fatalf("circuit audit transitions = %v", seen)
	}
}

func TestTransientFailuresAreBoundedAndSuccessResetsSequence(t *testing.T) {
	base := memory.New()
	health, err := meter.NewHealth(base, base, meter.HealthConfig{TransientFailureThreshold: 2})
	if err != nil {
		t.Fatalf("new health: %v", err)
	}
	busy := sqlite3.Error{Code: sqlite3.ErrBusy}
	health.RecordFailure(context.Background(), busy)
	if got := health.Snapshot(); got.State != meter.CircuitClosed || got.ConsecutiveTransient != 1 {
		t.Fatalf("first transient snapshot = %+v", got)
	}
	health.RecordSuccess()
	health.RecordFailure(context.Background(), busy)
	if got := health.Snapshot(); got.State != meter.CircuitClosed || got.ConsecutiveTransient != 1 {
		t.Fatalf("reset transient snapshot = %+v", got)
	}
	health.RecordFailure(context.Background(), busy)
	if got := health.Snapshot(); got.State != meter.CircuitOpen || got.TransientFailures != 3 {
		t.Fatalf("threshold snapshot = %+v", got)
	}
}

func TestFailedRecoveryProbeKeepsCircuitOpen(t *testing.T) {
	base := memory.New()
	controlled := &controlledStore{Store: base, probeErr: errors.New("still read only")}
	clock := &fakeClock{now: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)}
	health, err := meter.NewHealth(controlled, base, meter.HealthConfig{RecoveryCooldown: time.Second, Now: clock.Now})
	if err != nil {
		t.Fatalf("new health: %v", err)
	}
	health.RecordFailure(context.Background(), errors.New("write failed"))
	clock.Advance(time.Second)
	if err := health.BeforeInference(context.Background()); !errors.Is(err, meter.ErrUnavailable) {
		t.Fatalf("failed probe = %v", err)
	}
	if got := health.Snapshot(); got.State != meter.CircuitOpen || !got.RetryAt.After(clock.Now()) {
		t.Fatalf("failed probe snapshot = %+v", got)
	}
	controlled.setProbeError(nil)
	clock.Advance(time.Second)
	if err := health.BeforeInference(context.Background()); err != nil {
		t.Fatalf("second recovery probe: %v", err)
	}
}

func TestHalfOpenAllowsOnlyOneCommittedProbe(t *testing.T) {
	base := memory.New()
	controlled := &controlledStore{
		Store: base, probeEnter: make(chan struct{}, 1), probeBlock: make(chan struct{}),
	}
	clock := &fakeClock{now: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)}
	health, err := meter.NewHealth(controlled, base, meter.HealthConfig{RecoveryCooldown: time.Second, Now: clock.Now})
	if err != nil {
		t.Fatalf("new health: %v", err)
	}
	health.RecordFailure(context.Background(), errors.New("write failed"))
	clock.Advance(time.Second)

	firstDone := make(chan error, 1)
	go func() { firstDone <- health.BeforeInference(context.Background()) }()
	<-controlled.probeEnter
	if err := health.BeforeInference(context.Background()); !errors.Is(err, meter.ErrUnavailable) {
		t.Fatalf("second half-open caller = %v", err)
	}
	close(controlled.probeBlock)
	if err := <-firstDone; err != nil {
		t.Fatalf("probe caller failed: %v", err)
	}
	if controlled.probeCalls != 1 {
		t.Fatalf("probe calls = %d, want 1", controlled.probeCalls)
	}
}

func TestInFlightFailureWinsOverSuccessfulRecoveryProbe(t *testing.T) {
	base := memory.New()
	controlled := &controlledStore{
		Store: base, probeEnter: make(chan struct{}, 1), probeBlock: make(chan struct{}),
	}
	clock := &fakeClock{now: time.Date(2026, 7, 22, 12, 0, 0, 0, time.UTC)}
	health, err := meter.NewHealth(controlled, base, meter.HealthConfig{RecoveryCooldown: time.Second, Now: clock.Now})
	if err != nil {
		t.Fatalf("new health: %v", err)
	}
	health.RecordFailure(context.Background(), errors.New("initial write failed"))
	clock.Advance(time.Second)

	probeDone := make(chan error, 1)
	go func() { probeDone <- health.BeforeInference(context.Background()) }()
	<-controlled.probeEnter
	health.RecordFailure(context.Background(), errors.New("in-flight write failed"))
	close(controlled.probeBlock)
	if err := <-probeDone; !errors.Is(err, meter.ErrUnavailable) {
		t.Fatalf("probe result after concurrent failure = %v", err)
	}
	if got := health.Snapshot(); got.State != meter.CircuitOpen || got.OpenedTotal != 2 {
		t.Fatalf("concurrent failure snapshot = %+v", got)
	}
}

func TestRecorderFailureDrivesSharedHealth(t *testing.T) {
	base := memory.New()
	controlled := &controlledStore{Store: base, recordErr: errors.New("ledger unavailable")}
	health, err := meter.NewHealth(controlled, base, meter.HealthConfig{})
	if err != nil {
		t.Fatalf("new health: %v", err)
	}
	recorder := &meter.Recorder{Store: controlled, Health: health}
	token := store.Token{ID: "token-id", UserID: "user-id"}
	ctx := authmw.WithToken(context.Background(), token)
	recorder.RecordInference(ctx, "profile", nil, false, nil)
	if got := health.Snapshot(); got.State != meter.CircuitOpen || got.PersistentFailures != 1 {
		t.Fatalf("recorder did not open circuit: %+v", got)
	}
}
