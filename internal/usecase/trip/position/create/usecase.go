package create

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/eternal-despair/template/internal/domain/trip"
	"github.com/eternal-despair/template/internal/model"
)

type Usecase struct {
	repo      tripRepository
	txManager txManager
	canceler  queueCanceler
	metrics   metrics
}

func New(repo tripRepository, txManager txManager, canceler queueCanceler, metrics metrics) *Usecase {
	return &Usecase{
		repo:      repo,
		txManager: txManager,
		canceler:  canceler,
		metrics:   metrics,
	}
}

func (u *Usecase) Execute(ctx context.Context, input model.CreatePositionInput) (*model.TripPosition, error) {
	if input.TripID == uuid.Nil {
		return nil, trip.NewValidationError("trip_id", "trip id cannot be empty")
	}
	if err := trip.ValidateCoordinates(input.Coordinates, "coordinates"); err != nil {
		return nil, err
	}
	if input.RecordedAt.IsZero() {
		return nil, trip.NewValidationError("recorded_at", "recorded_at cannot be empty")
	}
	now := time.Now().UTC()
	if input.RecordedAt.After(now.Add(1 * time.Minute)) {
		return nil, trip.NewValidationError("recorded_at", "recorded_at cannot be in future more than 1 minute")
	}

	tr, err := u.repo.GetByID(ctx, input.TripID)
	if err != nil {
		return nil, err
	}
	if tr.Status != trip.StatusActive {
		return nil, trip.ErrTripCompleted
	}

	var saved *model.TripPosition
	err = u.txManager.Do(ctx, func(txCtx context.Context) error {
		res, saveErr := u.repo.SavePosition(txCtx, &input)
		if saveErr != nil {
			return saveErr
		}
		saved = res
		return nil
	})
	if err != nil {
		return nil, err
	}

	if u.canceler != nil {
		u.canceler.Cancel(input.TripID)
	}
	if u.metrics != nil {
		u.metrics.PositionSaved(ctx)
	}

	return saved, nil
}
