//go:build integration

package trip_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eternal-despair/template/internal/domain/trip"
	"github.com/eternal-despair/template/internal/infra/postgres/transaction"
	"github.com/eternal-despair/template/internal/model"
	repoPkg "github.com/eternal-despair/template/internal/repository/trip"
)

func setupTestDB(t *testing.T) (*pgxpool.Pool, *repoPkg.Repository, *transaction.Manager) {
	t.Helper()

	_ = godotenv.Load("../../../.env")
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL is not set, skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Skipf("cannot connect to test database at %s: %v", dbURL, err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("cannot ping test database at %s: %v", dbURL, err)
	}

	repo := repoPkg.New(pool)
	txMgr := transaction.New(pool)

	return pool, repo, txMgr
}

func cleanupTrip(ctx context.Context, pool *pgxpool.Pool, tripID uuid.UUID) {
	_, _ = pool.Exec(ctx, "DELETE FROM trip_positions WHERE trip_id = $1", tripID)
	_, _ = pool.Exec(ctx, "DELETE FROM trip_status_history WHERE trip_id = $1", tripID)
	_, _ = pool.Exec(ctx, "DELETE FROM trips WHERE id = $1", tripID)
}

func TestRepository_CreateAndGet(t *testing.T) {
	pool, repo, _ := setupTestDB(t)
	defer pool.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	tripID := uuid.New()
	driverID := uuid.New()
	userID := uuid.New()

	defer cleanupTrip(ctx, pool, tripID)

	tr := &model.Trip{
		ID:       tripID,
		UserID:   userID,
		DriverID: driverID,
		StartPoint: trip.Coordinates{
			Latitude:  55.751244,
			Longitude: 37.618423,
		},
		EndPoint: trip.Coordinates{
			Latitude:  55.755814,
			Longitude: 37.617635,
		},
		Price:     750,
		Status:    trip.StatusActive,
		StartedAt: now,
		CreatedAt: now,
		UpdatedAt: now,
	}

	err := repo.Create(ctx, tr)
	require.NoError(t, err)

	fetched, err := repo.GetByID(ctx, tripID)
	require.NoError(t, err)
	assert.Equal(t, tripID, fetched.ID)
	assert.Equal(t, userID, fetched.UserID)
	assert.Equal(t, driverID, fetched.DriverID)
	assert.Equal(t, int64(750), fetched.Price)
	assert.Equal(t, trip.StatusActive, fetched.Status)
	assert.Nil(t, fetched.FinishedAt)
}

func TestRepository_DuplicateDriverActiveTrip(t *testing.T) {
	pool, repo, _ := setupTestDB(t)
	defer pool.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	driverID := uuid.New()

	trip1ID := uuid.New()
	trip2ID := uuid.New()

	defer cleanupTrip(ctx, pool, trip1ID)
	defer cleanupTrip(ctx, pool, trip2ID)

	tr1 := &model.Trip{
		ID:         trip1ID,
		UserID:     uuid.New(),
		DriverID:   driverID,
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      500,
		Status:     trip.StatusActive,
		StartedAt:  now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	err := repo.Create(ctx, tr1)
	require.NoError(t, err)

	tr2 := &model.Trip{
		ID:         trip2ID,
		UserID:     uuid.New(),
		DriverID:   driverID,
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      600,
		Status:     trip.StatusActive,
		StartedAt:  now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	err = repo.Create(ctx, tr2)
	require.ErrorIs(t, err, trip.ErrDriverBusy)
}

func TestRepository_Finish(t *testing.T) {
	pool, repo, _ := setupTestDB(t)
	defer pool.Close()

	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Microsecond)
	tripID := uuid.New()

	defer cleanupTrip(ctx, pool, tripID)

	tr := &model.Trip{
		ID:         tripID,
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      1000,
		Status:     trip.StatusActive,
		StartedAt:  now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	err := repo.Create(ctx, tr)
	require.NoError(t, err)

	finishTime := now.Add(15 * time.Minute)
	finished, err := repo.Finish(ctx, tripID, finishTime)
	require.NoError(t, err)
	assert.Equal(t, trip.StatusCompleted, finished.Status)
	require.NotNil(t, finished.FinishedAt)

	// Повторное завершение уже завершенной поездки возвращает ErrTripCompleted
	_, err = repo.Finish(ctx, tripID, finishTime.Add(5*time.Minute))
	assert.ErrorIs(t, err, trip.ErrTripCompleted)
}

func TestRepository_GetNotFound(t *testing.T) {
	pool, repo, _ := setupTestDB(t)
	defer pool.Close()

	ctx := context.Background()
	randomID := uuid.New()

	_, err := repo.GetByID(ctx, randomID)
	assert.ErrorIs(t, err, trip.ErrTripNotFound)
}

func TestRepository_TransactionRollback(t *testing.T) {
	pool, repo, txMgr := setupTestDB(t)
	defer pool.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	tripID := uuid.New()

	defer cleanupTrip(ctx, pool, tripID)

	tr := &model.Trip{
		ID:         tripID,
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      800,
		Status:     trip.StatusActive,
		StartedAt:  now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	simulatedErr := errors.New("simulated error to trigger rollback")

	// 1. Проверяем откат по возврату ошибки
	err := txMgr.Do(ctx, func(txCtx context.Context) error {
		if err := repo.Create(txCtx, tr); err != nil {
			return err
		}
		// Симулируем падение перед записью истории
		return simulatedErr
	})
	require.ErrorIs(t, err, simulatedErr)

	// Поездка не должна была сохраниться в БД
	_, err = repo.GetByID(ctx, tripID)
	assert.ErrorIs(t, err, trip.ErrTripNotFound)

	// 2. Проверяем откат при панике
	assert.Panics(t, func() {
		_ = txMgr.Do(ctx, func(txCtx context.Context) error {
			_ = repo.Create(txCtx, tr)
			panic("unexpected runtime panic")
		})
	})

	// Поездка не должна была сохраниться в БД после паники
	_, err = repo.GetByID(ctx, tripID)
	assert.ErrorIs(t, err, trip.ErrTripNotFound)
}

func TestRepository_GetStaleTrips(t *testing.T) {
	pool, repo, _ := setupTestDB(t)
	defer pool.Close()

	ctx := context.Background()
	now := time.Now().UTC()

	staleTripID := uuid.New()
	freshTripID := uuid.New()
	completedTripID := uuid.New()

	defer cleanupTrip(ctx, pool, staleTripID)
	defer cleanupTrip(ctx, pool, freshTripID)
	defer cleanupTrip(ctx, pool, completedTripID)

	staleTrip := &model.Trip{
		ID:         staleTripID,
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      500,
		Status:     trip.StatusActive,
		StartedAt:  now.Add(-10 * time.Minute),
		CreatedAt:  now.Add(-10 * time.Minute),
		UpdatedAt:  now.Add(-10 * time.Minute),
	}
	require.NoError(t, repo.Create(ctx, staleTrip))

	freshTrip := &model.Trip{
		ID:         freshTripID,
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      600,
		Status:     trip.StatusActive,
		StartedAt:  now.Add(-10 * time.Minute),
		CreatedAt:  now.Add(-10 * time.Minute),
		UpdatedAt:  now.Add(-10 * time.Minute),
	}
	require.NoError(t, repo.Create(ctx, freshTrip))

	_, err := repo.SavePosition(ctx, &model.CreatePositionInput{
		TripID:      freshTripID,
		Coordinates: trip.Coordinates{Latitude: 55.751, Longitude: 37.611},
		RecordedAt:  now.Add(-10 * time.Second),
	})
	require.NoError(t, err)

	completedTrip := &model.Trip{
		ID:         completedTripID,
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      700,
		Status:     trip.StatusActive,
		StartedAt:  now.Add(-10 * time.Minute),
		CreatedAt:  now.Add(-10 * time.Minute),
		UpdatedAt:  now.Add(-10 * time.Minute),
	}
	require.NoError(t, repo.Create(ctx, completedTrip))
	_, err = repo.Finish(ctx, completedTripID, now.Add(-5*time.Minute))
	require.NoError(t, err)

	staleCutoff := now.Add(-1 * time.Minute)
	staleList, err := repo.GetStaleTrips(ctx, staleCutoff, 0)
	require.NoError(t, err)

	var foundStale, foundFresh, foundCompleted bool
	for _, tr := range staleList {
		if tr.ID == staleTripID {
			foundStale = true
		}
		if tr.ID == freshTripID {
			foundFresh = true
		}
		if tr.ID == completedTripID {
			foundCompleted = true
		}
	}

	assert.True(t, foundStale)
	assert.False(t, foundFresh)
	assert.False(t, foundCompleted)
}

func TestRepository_SavePositionAndRollback(t *testing.T) {
	pool, repo, txMgr := setupTestDB(t)
	defer pool.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	tripID := uuid.New()

	defer cleanupTrip(ctx, pool, tripID)

	tr := &model.Trip{
		ID:         tripID,
		UserID:     uuid.New(),
		DriverID:   uuid.New(),
		StartPoint: trip.Coordinates{Latitude: 55.75, Longitude: 37.61},
		EndPoint:   trip.Coordinates{Latitude: 55.76, Longitude: 37.62},
		Price:      500,
		Status:     trip.StatusActive,
		StartedAt:  now.Add(-10 * time.Minute),
		CreatedAt:  now.Add(-10 * time.Minute),
		UpdatedAt:  now.Add(-10 * time.Minute),
	}
	require.NoError(t, repo.Create(ctx, tr))

	t1 := now.Add(-5 * time.Minute).Truncate(time.Microsecond)
	pos1, err := repo.SavePosition(ctx, &model.CreatePositionInput{
		TripID:      tripID,
		Coordinates: trip.Coordinates{Latitude: 55.751, Longitude: 37.611},
		RecordedAt:  t1,
	})
	require.NoError(t, err)
	assert.NotZero(t, pos1.ID)

	fetched, err := repo.GetByID(ctx, tripID)
	require.NoError(t, err)
	require.NotNil(t, fetched.LastPositionAt)
	assert.True(t, t1.Equal(*fetched.LastPositionAt))

	tOlder := now.Add(-8 * time.Minute).Truncate(time.Microsecond)
	_, err = repo.SavePosition(ctx, &model.CreatePositionInput{
		TripID:      tripID,
		Coordinates: trip.Coordinates{Latitude: 55.752, Longitude: 37.612},
		RecordedAt:  tOlder,
	})
	require.NoError(t, err)

	fetched, err = repo.GetByID(ctx, tripID)
	require.NoError(t, err)
	require.NotNil(t, fetched.LastPositionAt)
	assert.True(t, t1.Equal(*fetched.LastPositionAt))

	positionsList, err := repo.ListPositions(ctx, tripID)
	require.NoError(t, err)
	require.Len(t, positionsList, 2)
	assert.True(t, tOlder.Equal(positionsList[0].RecordedAt))
	assert.True(t, t1.Equal(positionsList[1].RecordedAt))

	simulatedErr := errors.New("rollback simulated")
	tNewer := now.Add(-1 * time.Minute)
	err = txMgr.Do(ctx, func(txCtx context.Context) error {
		_, saveErr := repo.SavePosition(txCtx, &model.CreatePositionInput{
			TripID:      tripID,
			Coordinates: trip.Coordinates{Latitude: 55.753, Longitude: 37.613},
			RecordedAt:  tNewer,
		})
		if saveErr != nil {
			return saveErr
		}
		return simulatedErr
	})
	require.ErrorIs(t, err, simulatedErr)

	fetchedAfterRollback, err := repo.GetByID(ctx, tripID)
	require.NoError(t, err)
	require.NotNil(t, fetchedAfterRollback.LastPositionAt)
	assert.True(t, t1.Equal(*fetchedAfterRollback.LastPositionAt))

	positionsAfterRollback, err := repo.ListPositions(ctx, tripID)
	require.NoError(t, err)
	assert.Len(t, positionsAfterRollback, 2)
}
