package create_test

import (
	"bytes"
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
	handlerPkg "github.com/eternal-despair/template/internal/http/trip/position/create"
	"github.com/eternal-despair/template/internal/model"
)

type mockUsecase struct {
	err error
}

func (m *mockUsecase) Execute(_ context.Context, input model.CreatePositionInput) (*model.TripPosition, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &model.TripPosition{
		ID:          42,
		TripID:      input.TripID,
		Coordinates: input.Coordinates,
		RecordedAt:  input.RecordedAt,
		CreatedAt:   time.Now().UTC(),
	}, nil
}

func TestHandler_Handle_Success(t *testing.T) {
	tripID := uuid.New()
	uc := &mockUsecase{}
	h := handlerPkg.New(uc)

	reqBody := api.PositionData{
		Coordinates: api.Coordinates{
			Latitude:  59.935,
			Longitude: 30.335,
		},
		RecordedAt: time.Now().UTC(),
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+tripID.String()+"/positions", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	h.Handle(rec, req, tripID)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp api.TripPosition
	err = json.NewDecoder(rec.Body).Decode(&resp)
	require.NoError(t, err)
	assert.Equal(t, int64(42), resp.Id)
	assert.Equal(t, tripID, resp.TripId)
}

func TestHandler_Handle_TripCompleted(t *testing.T) {
	tripID := uuid.New()
	uc := &mockUsecase{err: trip.ErrTripCompleted}
	h := handlerPkg.New(uc)

	reqBody := api.PositionData{
		Coordinates: api.Coordinates{Latitude: 59.935, Longitude: 30.335},
		RecordedAt:  time.Now().UTC(),
	}
	bodyBytes, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+tripID.String()+"/positions", bytes.NewReader(bodyBytes))
	rec := httptest.NewRecorder()

	h.Handle(rec, req, tripID)

	assert.Equal(t, http.StatusConflict, rec.Code)
}
