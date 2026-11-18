package create

import (
	"github.com/google/uuid"

	"github.com/eternal-despair/template/internal/domain/trip"
	api "github.com/eternal-despair/template/internal/generated"
	"github.com/eternal-despair/template/internal/model"
)

func toModelInput(tripID uuid.UUID, req api.PositionData) model.CreatePositionInput {
	return model.CreatePositionInput{
		TripID: tripID,
		Coordinates: trip.Coordinates{
			Latitude:  req.Coordinates.Latitude,
			Longitude: req.Coordinates.Longitude,
		},
		RecordedAt: req.RecordedAt,
	}
}

func toAPIPosition(p *model.TripPosition) api.TripPosition {
	return api.TripPosition{
		Id:     p.ID,
		TripId: p.TripID,
		Coordinates: api.Coordinates{
			Latitude:  p.Coordinates.Latitude,
			Longitude: p.Coordinates.Longitude,
		},
		RecordedAt: p.RecordedAt,
	}
}
