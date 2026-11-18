package list

import (
	"context"

	"github.com/google/uuid"

	"github.com/eternal-despair/template/internal/domain/trip"
	"github.com/eternal-despair/template/internal/model"
)

type Usecase struct {
	repo tripRepository
}

func New(repo tripRepository) *Usecase {
	return &Usecase{repo: repo}
}

func (u *Usecase) Execute(ctx context.Context, tripID uuid.UUID) ([]*model.TripPosition, error) {
	if tripID == uuid.Nil {
		return nil, trip.NewValidationError("trip_id", "trip id cannot be empty")
	}

	_, err := u.repo.GetByID(ctx, tripID)
	if err != nil {
		return nil, err
	}

	return u.repo.ListPositions(ctx, tripID)
}
