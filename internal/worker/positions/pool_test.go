package positions_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"

	"github.com/eternal-despair/template/internal/domain/trip"
	"github.com/eternal-despair/template/internal/model"
	"github.com/eternal-despair/template/internal/worker/positions"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

type fakeTicker struct {
	ch chan time.Time
}

func newFakeTicker() *fakeTicker {
	return &fakeTicker{ch: make(chan time.Time, 10)}
}

func (t *fakeTicker) Chan() <-chan time.Time {
	return t.ch
}

func (t *fakeTicker) Tick() {
	t.ch <- time.Now()
}

func (t *fakeTicker) Stop() {}

type mockRepo struct {
	mu         sync.Mutex
	staleTrips []*model.Trip
	err        error
}

func (m *mockRepo) GetStaleTrips(_ context.Context, _ time.Time, _ int) ([]*model.Trip, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.staleTrips, m.err
}

type mockGateway struct {
	mu       sync.Mutex
	calls    []uuid.UUID
	callCond *sync.Cond
	err      error
	blockCh  chan struct{}
}

func newMockGateway() *mockGateway {
	m := &mockGateway{
		calls: make([]uuid.UUID, 0),
	}
	m.callCond = sync.NewCond(&m.mu)
	return m
}

func (m *mockGateway) RequestPosition(_ context.Context, _, tripID uuid.UUID) error {
	if m.blockCh != nil {
		<-m.blockCh
	}
	m.mu.Lock()
	m.calls = append(m.calls, tripID)
	m.callCond.Broadcast()
	err := m.err
	m.mu.Unlock()
	return err
}

func (m *mockGateway) WaitCalls(count int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	m.mu.Lock()
	defer m.mu.Unlock()
	for len(m.calls) < count {
		rem := time.Until(deadline)
		if rem <= 0 {
			return false
		}
		ch := make(chan struct{})
		go func() {
			m.mu.Lock()
			m.callCond.Wait()
			m.mu.Unlock()
			close(ch)
		}()
		m.mu.Unlock()
		select {
		case <-ch:
			m.mu.Lock()
		case <-time.After(rem):
			m.mu.Lock()
			return len(m.calls) >= count
		}
	}
	return true
}

type mockMetrics struct {
	mu               sync.Mutex
	staleChecks      map[string]int
	positionRequests map[string]int
}

func newMockMetrics() *mockMetrics {
	return &mockMetrics{
		staleChecks:      make(map[string]int),
		positionRequests: make(map[string]int),
	}
}

func (m *mockMetrics) RecordStaleCheck(_ context.Context, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.staleChecks[result]++
}

func (m *mockMetrics) RecordPositionRequest(_ context.Context, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.positionRequests[result]++
}

func (m *mockMetrics) RecordPositionRequestDuration(_ context.Context, _ string, _ float64) {}

func (m *mockMetrics) RecordQueueSize(_ context.Context, _ int64) {}

func (m *mockMetrics) RecordActiveWorkers(_ context.Context, _ int64) {}

func (m *mockMetrics) GetStaleCheckCount(res string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.staleChecks[res]
}

func (m *mockMetrics) GetPositionRequestCount(res string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.positionRequests[res]
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func makeTrip(id, driverID uuid.UUID) *model.Trip {
	now := time.Now().UTC()
	return &model.Trip{
		ID:        id,
		DriverID:  driverID,
		Status:    trip.StatusActive,
		StartedAt: now,
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func TestPool_Deduplication(t *testing.T) {
	ticker := newFakeTicker()
	trID := uuid.New()
	drvID := uuid.New()
	tripObj := makeTrip(trID, drvID)

	repo := &mockRepo{staleTrips: []*model.Trip{tripObj}}
	gw := newMockGateway()
	gw.blockCh = make(chan struct{})

	metrics := newMockMetrics()

	cfg := positions.Config{
		CheckInterval: 10 * time.Millisecond,
		StaleAfter:    1 * time.Minute,
		Workers:       1,
		QueueSize:     10,
		PushTimeout:   100 * time.Millisecond,
	}

	p := positions.NewPool(cfg, repo, gw, metrics, discardLogger(), nil, ticker)
	p.Start()

	ticker.Tick()
	require.Eventually(t, func() bool {
		return metrics.GetStaleCheckCount("queued") == 1
	}, 2*time.Second, 10*time.Millisecond)

	ticker.Tick()
	require.Eventually(t, func() bool {
		return metrics.GetStaleCheckCount("duplicate") == 1
	}, 2*time.Second, 10*time.Millisecond)

	close(gw.blockCh)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.Shutdown(shutdownCtx))
}

func TestPool_CancellationOnCoordinateArrival(t *testing.T) {
	ticker := newFakeTicker()
	tr1 := makeTrip(uuid.New(), uuid.New())
	tr2 := makeTrip(uuid.New(), uuid.New())

	repo := &mockRepo{staleTrips: []*model.Trip{tr1, tr2}}
	gw := newMockGateway()
	gw.blockCh = make(chan struct{})

	metrics := newMockMetrics()

	cfg := positions.Config{
		CheckInterval: 10 * time.Millisecond,
		StaleAfter:    1 * time.Minute,
		Workers:       1,
		QueueSize:     10,
		PushTimeout:   100 * time.Millisecond,
	}

	p := positions.NewPool(cfg, repo, gw, metrics, discardLogger(), nil, ticker)
	p.Start()

	ticker.Tick()

	require.Eventually(t, func() bool {
		return metrics.GetStaleCheckCount("queued") == 2
	}, 2*time.Second, 10*time.Millisecond)

	p.Cancel(tr2.ID)
	close(gw.blockCh)

	require.Eventually(t, func() bool {
		return metrics.GetPositionRequestCount("cancelled") == 1
	}, 2*time.Second, 10*time.Millisecond)

	gw.mu.Lock()
	assert.Equal(t, []uuid.UUID{tr1.ID}, gw.calls)
	gw.mu.Unlock()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.Shutdown(shutdownCtx))
}

func TestPool_QueueOverflowDroppedNotBlocked(t *testing.T) {
	ticker := newFakeTicker()
	gw := newMockGateway()
	gw.blockCh = make(chan struct{})

	trips := make([]*model.Trip, 5)
	for i := 0; i < 5; i++ {
		trips[i] = makeTrip(uuid.New(), uuid.New())
	}

	repo := &mockRepo{staleTrips: trips}
	metrics := newMockMetrics()

	cfg := positions.Config{
		CheckInterval: 10 * time.Millisecond,
		StaleAfter:    1 * time.Minute,
		Workers:       1,
		QueueSize:     2,
		PushTimeout:   100 * time.Millisecond,
	}

	p := positions.NewPool(cfg, repo, gw, metrics, discardLogger(), nil, ticker)
	p.Start()

	ticker.Tick()

	require.Eventually(t, func() bool {
		return metrics.GetStaleCheckCount("dropped") == 2
	}, 2*time.Second, 10*time.Millisecond)

	assert.Equal(t, 3, metrics.GetStaleCheckCount("queued"))
	assert.Equal(t, 2, metrics.GetStaleCheckCount("dropped"))

	close(gw.blockCh)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.Shutdown(shutdownCtx))
}

func TestPool_WorkerPoolProcessesTasks(t *testing.T) {
	ticker := newFakeTicker()
	const numTasks = 8
	trips := make([]*model.Trip, numTasks)
	for i := 0; i < numTasks; i++ {
		trips[i] = makeTrip(uuid.New(), uuid.New())
	}

	repo := &mockRepo{staleTrips: trips}
	gw := newMockGateway()
	metrics := newMockMetrics()

	cfg := positions.Config{
		CheckInterval: 10 * time.Millisecond,
		StaleAfter:    1 * time.Minute,
		Workers:       4,
		QueueSize:     20,
		PushTimeout:   500 * time.Millisecond,
	}

	p := positions.NewPool(cfg, repo, gw, metrics, discardLogger(), nil, ticker)
	p.Start()

	ticker.Tick()

	ok := gw.WaitCalls(numTasks, 3*time.Second)
	require.True(t, ok)

	require.Eventually(t, func() bool {
		return metrics.GetPositionRequestCount("success") == numTasks
	}, 2*time.Second, 10*time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.Shutdown(shutdownCtx))
}

func TestPool_ErrorHandlingDoesNotCrashWorker(t *testing.T) {
	ticker := newFakeTicker()
	tr1 := makeTrip(uuid.New(), uuid.New())
	tr2 := makeTrip(uuid.New(), uuid.New())

	repo := &mockRepo{staleTrips: []*model.Trip{tr1, tr2}}
	gw := newMockGateway()
	gw.err = errors.New("simulated network failure")

	metrics := newMockMetrics()

	cfg := positions.Config{
		CheckInterval: 10 * time.Millisecond,
		StaleAfter:    1 * time.Minute,
		Workers:       1,
		QueueSize:     10,
		PushTimeout:   100 * time.Millisecond,
	}

	p := positions.NewPool(cfg, repo, gw, metrics, discardLogger(), nil, ticker)
	p.Start()

	ticker.Tick()

	require.Eventually(t, func() bool {
		return metrics.GetPositionRequestCount("error") == 2
	}, 2*time.Second, 10*time.Millisecond)

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.Shutdown(shutdownCtx))
}

func TestPool_CleanShutdownNoLeaks(t *testing.T) {
	ticker := newFakeTicker()
	repo := &mockRepo{}
	gw := newMockGateway()
	metrics := newMockMetrics()

	cfg := positions.Config{
		CheckInterval: 10 * time.Millisecond,
		StaleAfter:    1 * time.Minute,
		Workers:       4,
		QueueSize:     10,
		PushTimeout:   100 * time.Millisecond,
	}

	p := positions.NewPool(cfg, repo, gw, metrics, discardLogger(), nil, ticker)
	p.Start()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, p.Shutdown(shutdownCtx))
}
