package list_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eternal-despair/template/internal/domain/trip"
	api "github.com/eternal-despair/template/internal/generated"
	handlerPkg "github.com/eternal-despair/template/internal/http/trip/position/list"
	"github.com/eternal-despair/template/internal/model"
)

type mockUsecase struct {
	positions []*model.TripPosition
	err       error
}

func (m *mockUsecase) Execute(_ context.Context, _ uuid.UUID) ([]*model.TripPosition, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.positions, nil
}

func TestHandler_Handle_Success(t *testing.T) {
	tripID := uuid.New()
	now := time.Now().UTC()

	posList := []*model.TripPosition{
		{
			ID:          1,
			TripID:      tripID,
			Coordinates: trip.Coordinates{Latitude: 59.93, Longitude: 30.33},
			RecordedAt:  now,
			CreatedAt:   now,
		},
	}

	uc := &mockUsecase{positions: posList}
	h := handlerPkg.New(uc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID.String()+"/positions", nil)
	rec := httptest.NewRecorder()

	h.Handle(rec, req, tripID)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp api.TripPositionsResponse
	err := json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Len(t, resp.Positions, 1)
	assert.Equal(t, int64(1), resp.Positions[0].Id)
}

func TestHandler_Handle_NotFound(t *testing.T) {
	tripID := uuid.New()
	uc := &mockUsecase{err: trip.ErrTripNotFound}
	h := handlerPkg.New(uc)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+tripID.String()+"/positions", nil)
	rec := httptest.NewRecorder()

	h.Handle(rec, req, tripID)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}
