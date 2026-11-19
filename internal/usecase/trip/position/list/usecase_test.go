package list_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eternal-despair/template/internal/domain/trip"
	"github.com/eternal-despair/template/internal/model"
	usecasePkg "github.com/eternal-despair/template/internal/usecase/trip/position/list"
)

type mockRepo struct {
	trip      *model.Trip
	positions []*model.TripPosition
	err       error
}

func (m *mockRepo) GetByID(_ context.Context, _ uuid.UUID) (*model.Trip, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.trip, nil
}

func (m *mockRepo) ListPositions(_ context.Context, _ uuid.UUID) ([]*model.TripPosition, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.positions, nil
}

func TestUsecase_Execute_Success(t *testing.T) {
	tripID := uuid.New()
	now := time.Now().UTC()

	activeTrip := &model.Trip{
		ID:     tripID,
		Status: trip.StatusActive,
	}

	positionsList := []*model.TripPosition{
		{
			ID:          1,
			TripID:      tripID,
			Coordinates: trip.Coordinates{Latitude: 59.93, Longitude: 30.33},
			RecordedAt:  now.Add(-5 * time.Minute),
		},
		{
			ID:          2,
			TripID:      tripID,
			Coordinates: trip.Coordinates{Latitude: 59.94, Longitude: 30.34},
			RecordedAt:  now,
		},
	}

	repo := &mockRepo{
		trip:      activeTrip,
		positions: positionsList,
	}

	uc := usecasePkg.New(repo)
	res, err := uc.Execute(context.Background(), tripID)
	require.NoError(t, err)
	assert.Len(t, res, 2)
	assert.Equal(t, int64(1), res[0].ID)
	assert.Equal(t, int64(2), res[1].ID)
}

func TestUsecase_Execute_NotFound(t *testing.T) {
	repo := &mockRepo{err: trip.ErrTripNotFound}
	uc := usecasePkg.New(repo)

	_, err := uc.Execute(context.Background(), uuid.New())
	require.ErrorIs(t, err, trip.ErrTripNotFound)
}
