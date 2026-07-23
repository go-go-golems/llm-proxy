package meter

import (
	"context"
	stderrors "errors"
	"sync"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/pkg/errors"

	"github.com/go-go-golems/llm-proxy/pkg/byok/store"
)

var ErrUnavailable = errors.New("BYOK metering is unavailable")

type CircuitState string

const (
	CircuitClosed   CircuitState = "closed"
	CircuitOpen     CircuitState = "open"
	CircuitHalfOpen CircuitState = "half_open"
)

type FailureClass string

const (
	FailureTransient  FailureClass = "transient"
	FailurePersistent FailureClass = "persistent"
)

type HealthConfig struct {
	TransientFailureThreshold int
	RecoveryCooldown          time.Duration
	Now                       func() time.Time
}

type HealthSnapshot struct {
	State                CircuitState
	ConsecutiveTransient int
	TransientFailures    uint64
	PersistentFailures   uint64
	OpenedTotal          uint64
	RecoveredTotal       uint64
	RetryAt              time.Time
}

// Health coordinates the usage recorder, request middleware, readiness, a
// committed store write probe, and typed audit transitions. Persistent failures
// open immediately. Transient SQLite busy/locked failures open only after a
// bounded threshold. Recovery requires a successful committed write probe
// before another inference is dispatched.
type Health struct {
	probe store.MeterStore
	audit store.AuditStore
	cfg   HealthConfig

	mu                   sync.Mutex
	state                CircuitState
	consecutiveTransient int
	transientFailures    uint64
	persistentFailures   uint64
	openedTotal          uint64
	recoveredTotal       uint64
	retryAt              time.Time
}

func NewHealth(probe store.MeterStore, audit store.AuditStore, cfg HealthConfig) (*Health, error) {
	if probe == nil {
		return nil, errors.New("metering health probe is required")
	}
	if cfg.TransientFailureThreshold == 0 {
		cfg.TransientFailureThreshold = 3
	}
	if cfg.TransientFailureThreshold < 1 {
		return nil, errors.New("metering transient failure threshold must be positive")
	}
	if cfg.RecoveryCooldown == 0 {
		cfg.RecoveryCooldown = 5 * time.Second
	}
	if cfg.RecoveryCooldown < 0 {
		return nil, errors.New("metering recovery cooldown must not be negative")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Health{probe: probe, audit: audit, cfg: cfg, state: CircuitClosed}, nil
}

// BeforeInference rejects requests while open. Once the cooldown expires,
// exactly one caller performs a committed write probe. No inference is allowed
// through until that probe succeeds.
func (h *Health) BeforeInference(ctx context.Context) error {
	if h == nil {
		return nil
	}
	now := h.cfg.Now().UTC()
	h.mu.Lock()
	switch h.state {
	case CircuitClosed:
		h.mu.Unlock()
		return nil
	case CircuitHalfOpen:
		h.mu.Unlock()
		return ErrUnavailable
	case CircuitOpen:
		if now.Before(h.retryAt) {
			h.mu.Unlock()
			return ErrUnavailable
		}
		h.state = CircuitHalfOpen
	default:
		h.mu.Unlock()
		return ErrUnavailable
	}
	h.mu.Unlock()

	if err := h.probe.CheckMeteringHealth(ctx); err != nil {
		h.mu.Lock()
		if classifyFailure(err) == FailureTransient {
			h.transientFailures++
			h.consecutiveTransient++
		} else {
			h.persistentFailures++
		}
		h.state = CircuitOpen
		h.retryAt = h.cfg.Now().UTC().Add(h.cfg.RecoveryCooldown)
		h.mu.Unlock()
		return ErrUnavailable
	}

	transitionAt := h.cfg.Now().UTC()
	h.mu.Lock()
	// A request already in flight may report a new accounting failure while
	// this probe runs. That failure moves the state back to open and wins over
	// the successful probe.
	if h.state != CircuitHalfOpen {
		h.mu.Unlock()
		return ErrUnavailable
	}
	h.state = CircuitClosed
	h.consecutiveTransient = 0
	h.retryAt = time.Time{}
	h.recoveredTotal++
	h.mu.Unlock()
	h.emit(ctx, store.AuditMeterCircuitClosed, store.MeterCircuitPayload{
		From: string(CircuitHalfOpen), To: string(CircuitClosed), Reason: "committed_write_probe_succeeded",
	}, transitionAt)
	return nil
}

// RecordFailure updates circuit state after a failed usage write.
func (h *Health) RecordFailure(ctx context.Context, err error) {
	if h == nil || err == nil {
		return
	}
	class := classifyFailure(err)
	now := h.cfg.Now().UTC()
	opened := false
	reason := "persistent_write_failure"
	h.mu.Lock()
	from := h.state
	switch class {
	case FailureTransient:
		h.transientFailures++
		h.consecutiveTransient++
		reason = "transient_failure_threshold"
		opened = h.state == CircuitHalfOpen || (h.state == CircuitClosed && h.consecutiveTransient >= h.cfg.TransientFailureThreshold)
	case FailurePersistent:
		h.persistentFailures++
		opened = h.state == CircuitClosed || h.state == CircuitHalfOpen
	}
	if opened {
		h.state = CircuitOpen
		h.retryAt = now.Add(h.cfg.RecoveryCooldown)
		h.openedTotal++
	}
	h.mu.Unlock()
	if opened {
		h.emit(ctx, store.AuditMeterCircuitOpened, store.MeterCircuitPayload{
			From: string(from), To: string(CircuitOpen), Reason: reason,
		}, now)
	}
}

// RecordSuccess resets transient failures only while closed. A late success
// from a request that started before another request opened the circuit cannot
// close it; recovery always requires the explicit write probe.
func (h *Health) RecordSuccess() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state == CircuitClosed {
		h.consecutiveTransient = 0
	}
}

func (h *Health) Ready(context.Context) error {
	if h == nil {
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.state != CircuitClosed {
		return ErrUnavailable
	}
	return nil
}

func (h *Health) Snapshot() HealthSnapshot {
	if h == nil {
		return HealthSnapshot{State: CircuitClosed}
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return HealthSnapshot{
		State: h.state, ConsecutiveTransient: h.consecutiveTransient,
		TransientFailures: h.transientFailures, PersistentFailures: h.persistentFailures,
		OpenedTotal: h.openedTotal, RecoveredTotal: h.recoveredTotal, RetryAt: h.retryAt,
	}
}

func (h *Health) emit(ctx context.Context, eventType string, payload store.MeterCircuitPayload, at time.Time) {
	if h.audit == nil {
		return
	}
	if err := h.audit.AppendEvent(context.WithoutCancel(ctx), store.MeterCircuitEvent(eventType, payload, at)); err != nil {
		log.Warn().Str("event", eventType).Msg("byok: metering circuit audit delivery failed")
	}
}

func classifyFailure(err error) FailureClass {
	if stderrors.Is(err, context.Canceled) || stderrors.Is(err, context.DeadlineExceeded) {
		return FailureTransient
	}
	var sqliteErr sqlite3.Error
	if stderrors.As(err, &sqliteErr) && (sqliteErr.Code == sqlite3.ErrBusy || sqliteErr.Code == sqlite3.ErrLocked) {
		return FailureTransient
	}
	return FailurePersistent
}
