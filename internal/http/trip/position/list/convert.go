package list

import (
	api "github.com/eternal-despair/template/internal/generated"
	"github.com/eternal-despair/template/internal/model"
)

func toAPIPositions(positions []*model.TripPosition) []api.TripPosition {
	if len(positions) == 0 {
		return []api.TripPosition{}
	}
	res := make([]api.TripPosition, len(positions))
	for i, p := range positions {
		res[i] = api.TripPosition{
			Id:     p.ID,
			TripId: p.TripID,
			Coordinates: api.Coordinates{
				Latitude:  p.Coordinates.Latitude,
				Longitude: p.Coordinates.Longitude,
			},
			RecordedAt: p.RecordedAt,
		}
	}
	return res
}
