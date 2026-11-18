package list

import (
	"context"

	"github.com/google/uuid"

	"github.com/eternal-despair/template/internal/model"
)

type usecase interface {
	Execute(ctx context.Context, tripID uuid.UUID) ([]*model.TripPosition, error)
}
