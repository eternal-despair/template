package create

import (
	"context"

	"github.com/google/uuid"

	"github.com/eternal-despair/template/internal/model"
)

type tripRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	SavePosition(ctx context.Context, pos *model.CreatePositionInput) (*model.TripPosition, error)
}

type txManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type queueCanceler interface {
	Cancel(tripID uuid.UUID)
}

type metrics interface {
	PositionSaved(ctx context.Context)
}
