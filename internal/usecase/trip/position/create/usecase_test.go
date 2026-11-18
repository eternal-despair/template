package create_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eternal-despair/template/internal/domain/trip"
	"github.com/eternal-despair/template/internal/model"
	usecasePkg "github.com/eternal-despair/template/internal/usecase/trip/position/create"
)

type mockRepo struct {
	trip     *model.Trip
	savedPos *model.TripPosition
	err      error
}

func (m *mockRepo) GetByID(_ context.Context, _ uuid.UUID) (*model.Trip, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.trip, nil
}

func (m *mockRepo) SavePosition(_ context.Context, pos *model.CreatePositionInput) (*model.TripPosition, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.savedPos = &model.TripPosition{
		ID:          1,
		TripID:      pos.TripID,
		Coordinates: pos.Coordinates,
		RecordedAt:  pos.RecordedAt,
		CreatedAt:   time.Now().UTC(),
	}
	return m.savedPos, nil
}

type mockTxManager struct{}

func (m *mockTxManager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return fn(ctx)
}

type mockCanceler struct {
	canceledID uuid.UUID
}

func (m *mockCanceler) Cancel(id uuid.UUID) {
	m.canceledID = id
}

type mockMetrics struct {
	savedCount int
}

func (m *mockMetrics) PositionSaved(_ context.Context) {
	m.savedCount++
}

func TestUsecase_Execute_Success(t *testing.T) {
	tripID := uuid.New()
	driverID := uuid.New()
	now := time.Now().UTC()

	activeTrip := &model.Trip{
		ID:        tripID,
		DriverID:  driverID,
		Status:    trip.StatusActive,
		StartedAt: now.Add(-10 * time.Minute),
	}

	repo := &mockRepo{trip: activeTrip}
	txMgr := &mockTxManager{}
	canceler := &mockCanceler{}
	metrics := &mockMetrics{}

	uc := usecasePkg.New(repo, txMgr, canceler, metrics)

	input := model.CreatePositionInput{
		TripID:      tripID,
		Coordinates: trip.Coordinates{Latitude: 59.93, Longitude: 30.33},
		RecordedAt:  now,
	}

	pos, err := uc.Execute(context.Background(), input)
	require.NoError(t, err)
	assert.Equal(t, int64(1), pos.ID)
	assert.Equal(t, tripID, canceler.canceledID)
	assert.Equal(t, 1, metrics.savedCount)
}

func TestUsecase_Execute_ValidationErrors(t *testing.T) {
	uc := usecasePkg.New(nil, nil, nil, nil)

	_, err := uc.Execute(context.Background(), model.CreatePositionInput{
		TripID: uuid.Nil,
	})
	require.Error(t, err)

	_, err = uc.Execute(context.Background(), model.CreatePositionInput{
		TripID:      uuid.New(),
		Coordinates: trip.Coordinates{Latitude: 100, Longitude: 30},
	})
	require.Error(t, err)

	_, err = uc.Execute(context.Background(), model.CreatePositionInput{
		TripID:      uuid.New(),
		Coordinates: trip.Coordinates{Latitude: 50, Longitude: 30},
		RecordedAt:  time.Now().UTC().Add(2 * time.Minute),
	})
	require.Error(t, err)
}

func TestUsecase_Execute_TripCompleted(t *testing.T) {
	tripID := uuid.New()
	completedTrip := &model.Trip{
		ID:     tripID,
		Status: trip.StatusCompleted,
	}

	repo := &mockRepo{trip: completedTrip}
	uc := usecasePkg.New(repo, &mockTxManager{}, nil, nil)

	input := model.CreatePositionInput{
		TripID:      tripID,
		Coordinates: trip.Coordinates{Latitude: 59.93, Longitude: 30.33},
		RecordedAt:  time.Now().UTC(),
	}

	_, err := uc.Execute(context.Background(), input)
	require.ErrorIs(t, err, trip.ErrTripCompleted)
}
