package positions

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/eternal-despair/template/internal/model"
)

type PushGateway interface {
	RequestPosition(ctx context.Context, driverID, tripID uuid.UUID) error
}

type TripRepository interface {
	GetStaleTrips(ctx context.Context, staleBefore time.Time, limit int) ([]*model.Trip, error)
}

type Ticker interface {
	Chan() <-chan time.Time
	Stop()
}

type realTicker struct {
	t *time.Ticker
}

func NewRealTicker(d time.Duration) Ticker {
	return &realTicker{t: time.NewTicker(d)}
}

func (r *realTicker) Chan() <-chan time.Time {
	return r.t.C
}

func (r *realTicker) Stop() {
	r.t.Stop()
}

type Metrics interface {
	RecordStaleCheck(ctx context.Context, result string)
	RecordPositionRequest(ctx context.Context, result string)
	RecordPositionRequestDuration(ctx context.Context, result string, durationSec float64)
	RecordQueueSize(ctx context.Context, size int64)
	RecordActiveWorkers(ctx context.Context, count int64)
}
