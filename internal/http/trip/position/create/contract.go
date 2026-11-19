package create

import (
	"context"

	"github.com/eternal-despair/template/internal/model"
)

type usecase interface {
	Execute(ctx context.Context, input model.CreatePositionInput) (*model.TripPosition, error)
}
