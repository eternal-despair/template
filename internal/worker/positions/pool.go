package positions

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/eternal-despair/template/internal/model"
)

type Config struct {
	CheckInterval time.Duration
	StaleAfter    time.Duration
	Workers       int
	QueueSize     int
	PushTimeout   time.Duration
}

type task struct {
	tripID           uuid.UUID
	driverID         uuid.UUID
	checkSpanContext trace.SpanContext
}

type tracker struct {
	mu     sync.Mutex
	queued map[uuid.UUID]bool
}

type Pool struct {
	cfg     Config
	repo    TripRepository
	gateway PushGateway
	metrics Metrics
	logger  *slog.Logger
	tracer  trace.Tracer

	ticker Ticker

	tracker *tracker
	tasksCh chan task

	activeWorkers   int64
	activeWorkersMu sync.Mutex

	scannerStopCh chan struct{}
	scannerDoneCh chan struct{}

	isScanning atomic.Bool

	workersWg sync.WaitGroup

	stopOnce sync.Once
}

func NewPool(
	cfg Config,
	repo TripRepository,
	gateway PushGateway,
	metrics Metrics,
	logger *slog.Logger,
	tracer trace.Tracer,
	ticker Ticker,
) *Pool {
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if cfg.QueueSize <= 0 {
		cfg.QueueSize = 100
	}
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = 30 * time.Second
	}
	if cfg.StaleAfter <= 0 {
		cfg.StaleAfter = 60 * time.Second
	}
	if cfg.PushTimeout <= 0 {
		cfg.PushTimeout = 1 * time.Second
	}

	if ticker == nil {
		ticker = NewRealTicker(cfg.CheckInterval)
	}

	return &Pool{
		cfg:           cfg,
		repo:          repo,
		gateway:       gateway,
		metrics:       metrics,
		logger:        logger,
		tracer:        tracer,
		ticker:        ticker,
		tracker:       &tracker{queued: make(map[uuid.UUID]bool)},
		tasksCh:       make(chan task, cfg.QueueSize),
		scannerStopCh: make(chan struct{}),
		scannerDoneCh: make(chan struct{}),
	}
}

func (p *Pool) Start() {
	p.logger.Info("starting stale positions worker pool",
		slog.Int("workers", p.cfg.Workers),
		slog.Int("queue_size", p.cfg.QueueSize),
	)

	for i := 0; i < p.cfg.Workers; i++ {
		p.workersWg.Add(1)
		go p.workerLoop()
	}

	go p.scannerLoop()
}

func (p *Pool) Cancel(tripID uuid.UUID) {
	p.tracker.mu.Lock()
	defer p.tracker.mu.Unlock()
	delete(p.tracker.queued, tripID)
}

func (p *Pool) scannerLoop() {
	defer close(p.scannerDoneCh)

	for {
		select {
		case <-p.scannerStopCh:
			return
		case <-p.ticker.Chan():
			p.runScan()
		}
	}
}

func (p *Pool) runScan() {
	if !p.isScanning.CompareAndSwap(false, true) {
		return
	}
	defer p.isScanning.Store(false)

	ctx := context.Background()
	var span trace.Span
	if p.tracer != nil {
		ctx, span = p.tracer.Start(ctx, "positions.scanner.tick")
		defer span.End()
	}

	staleBefore := time.Now().Add(-p.cfg.StaleAfter)
	staleTrips, err := p.repo.GetStaleTrips(ctx, staleBefore, 0)
	if err != nil {
		p.logger.Error("failed to fetch stale trips", slog.Any("error", err))
		if span != nil {
			span.RecordError(err)
		}
		return
	}

	var checkSpanCtx trace.SpanContext
	if span != nil {
		checkSpanCtx = span.SpanContext()
	}

	for _, tr := range staleTrips {
		select {
		case <-p.scannerStopCh:
			return
		default:
		}

		p.enqueueTrip(ctx, tr, checkSpanCtx)
	}
}

func (p *Pool) enqueueTrip(ctx context.Context, tr *model.Trip, checkSpanCtx trace.SpanContext) {
	p.tracker.mu.Lock()
	defer p.tracker.mu.Unlock()

	if p.tracker.queued[tr.ID] {
		p.metrics.RecordStaleCheck(ctx, "duplicate")
		return
	}

	t := task{
		tripID:           tr.ID,
		driverID:         tr.DriverID,
		checkSpanContext: checkSpanCtx,
	}

	select {
	case p.tasksCh <- t:
		p.tracker.queued[tr.ID] = true
		p.metrics.RecordStaleCheck(ctx, "queued")
		p.metrics.RecordQueueSize(ctx, int64(len(p.tasksCh)))
	default:
		p.metrics.RecordStaleCheck(ctx, "dropped")
		p.logger.Warn("stale trip check dropped: queue full", slog.String("trip_id", tr.ID.String()))
	}
}

func (p *Pool) workerLoop() {
	defer p.workersWg.Done()

	for t := range p.tasksCh {
		p.metrics.RecordQueueSize(context.Background(), int64(len(p.tasksCh)))

		p.tracker.mu.Lock()
		active := p.tracker.queued[t.tripID]
		if !active {
			p.tracker.mu.Unlock()
			p.metrics.RecordPositionRequest(context.Background(), "cancelled")
			continue
		}
		p.tracker.mu.Unlock()

		p.activeWorkersMu.Lock()
		p.activeWorkers++
		p.metrics.RecordActiveWorkers(context.Background(), p.activeWorkers)
		p.activeWorkersMu.Unlock()

		p.processTask(t)

		p.tracker.mu.Lock()
		delete(p.tracker.queued, t.tripID)
		p.tracker.mu.Unlock()

		p.activeWorkersMu.Lock()
		p.activeWorkers--
		p.metrics.RecordActiveWorkers(context.Background(), p.activeWorkers)
		p.activeWorkersMu.Unlock()
	}
}

func (p *Pool) processTask(t task) {
	var opts []trace.SpanStartOption
	if t.checkSpanContext.IsValid() {
		opts = append(opts, trace.WithLinks(trace.Link{SpanContext: t.checkSpanContext}))
	}

	ctx := context.Background()
	var span trace.Span
	if p.tracer != nil {
		ctx, span = p.tracer.Start(ctx, "positions.worker.request", opts...)
		defer span.End()
	}

	reqCtx, cancel := context.WithTimeout(ctx, p.cfg.PushTimeout)
	defer cancel()

	start := time.Now()
	err := p.gateway.RequestPosition(reqCtx, t.driverID, t.tripID)
	duration := time.Since(start)

	if err != nil {
		p.metrics.RecordPositionRequest(ctx, "error")
		p.metrics.RecordPositionRequestDuration(ctx, "error", duration.Seconds())
		p.logger.Warn("push service position request failed",
			slog.String("trip_id", t.tripID.String()),
			slog.Any("error", err),
		)
		if span != nil {
			span.RecordError(err)
		}
		return
	}

	p.metrics.RecordPositionRequest(ctx, "success")
	p.metrics.RecordPositionRequestDuration(ctx, "success", duration.Seconds())
}

func (p *Pool) StopScanner() {
	p.stopOnce.Do(func() {
		p.ticker.Stop()
		close(p.scannerStopCh)
		<-p.scannerDoneCh
		close(p.tasksCh)
	})
}

func (p *Pool) Shutdown(ctx context.Context) error {
	p.logger.Info("shutting down positions worker pool")
	p.StopScanner()

	done := make(chan struct{})
	go func() {
		p.workersWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		p.logger.Info("positions worker pool stopped cleanly")
		return nil
	case <-ctx.Done():
		return errors.New("positions worker pool shutdown timed out")
	}
}
