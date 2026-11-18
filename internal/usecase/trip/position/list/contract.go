package list

import (
	"context"

	"github.com/google/uuid"

	"github.com/eternal-despair/template/internal/model"
)

type tripRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	ListPositions(ctx context.Context, tripID uuid.UUID) ([]*model.TripPosition, error)
}
