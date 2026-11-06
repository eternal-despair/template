package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/eternal-despair/template/internal/model"
	"github.com/eternal-despair/template/internal/txmanager"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const pgErrUniqueViolation = "23505"

type TripRepository interface {
	Create(ctx context.Context, trip *model.Trip) error
	CreateStatusHistory(ctx context.Context, history *model.TripStatusHistory) error
	GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) (*model.Trip, error)
}

type tripRepository struct {
	pool    *pgxpool.Pool
	builder sq.StatementBuilderType
}

func NewTripRepository(pool *pgxpool.Pool) TripRepository {
	return &tripRepository{
		pool:    pool,
		builder: sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

func (r *tripRepository) Create(ctx context.Context, trip *model.Trip) error {
	executor := txmanager.GetExecutor(ctx, r.pool)

	query, args, err := r.builder.
		Insert("trips").
		Columns(
			"id",
			"user_id",
			"driver_id",
			"start_latitude",
			"start_longitude",
			"end_latitude",
			"end_longitude",
			"price",
			"status",
			"started_at",
			"created_at",
			"updated_at",
		).
		Values(
			trip.ID,
			trip.UserID,
			trip.DriverID,
			trip.StartPoint.Latitude,
			trip.StartPoint.Longitude,
			trip.EndPoint.Latitude,
			trip.EndPoint.Longitude,
			trip.Price,
			string(trip.Status),
			trip.StartedAt,
			trip.CreatedAt,
			trip.UpdatedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip query: %w", err)
	}

	_, err = executor.Exec(ctx, query, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrUniqueViolation {
			// Нарушение частичного уникального индекса trips_driver_active_idx
			return model.ErrDriverBusy
		}
		return fmt.Errorf("exec insert trip: %w", err)
	}

	return nil
}

func (r *tripRepository) CreateStatusHistory(ctx context.Context, history *model.TripStatusHistory) error {
	executor := txmanager.GetExecutor(ctx, r.pool)

	query, args, err := r.builder.
		Insert("trip_status_history").
		Columns(
			"trip_id",
			"from_status",
			"to_status",
			"reason",
			"changed_at",
		).
		Values(
			history.TripID,
			history.FromStatus,
			string(history.ToStatus),
			history.Reason,
			history.ChangedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert status history query: %w", err)
	}

	_, err = executor.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("exec insert status history: %w", err)
	}

	return nil
}

func (r *tripRepository) GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	executor := txmanager.GetExecutor(ctx, r.pool)

	query, args, err := r.builder.
		Select(
			"id",
			"user_id",
			"driver_id",
			"start_latitude",
			"start_longitude",
			"end_latitude",
			"end_longitude",
			"price",
			"status",
			"started_at",
			"finished_at",
			"created_at",
			"updated_at",
		).
		From("trips").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build get trip query: %w", err)
	}

	row := executor.QueryRow(ctx, query, args...)

	var trip model.Trip
	var status string
	err = row.Scan(
		&trip.ID,
		&trip.UserID,
		&trip.DriverID,
		&trip.StartPoint.Latitude,
		&trip.StartPoint.Longitude,
		&trip.EndPoint.Latitude,
		&trip.EndPoint.Longitude,
		&trip.Price,
		&status,
		&trip.StartedAt,
		&trip.FinishedAt,
		&trip.CreatedAt,
		&trip.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, model.ErrNotFound
		}
		return nil, fmt.Errorf("scan trip: %w", err)
	}

	trip.Status = model.TripStatus(status)
	return &trip, nil
}

func (r *tripRepository) Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) (*model.Trip, error) {
	executor := txmanager.GetExecutor(ctx, r.pool)

	// Завершение поездки одним атомарным запросом с WHERE status = 'active'
	query, args, err := r.builder.
		Update("trips").
		Set("status", string(model.TripStatusCompleted)).
		Set("finished_at", finishedAt).
		Set("updated_at", finishedAt).
		Where(sq.Eq{"id": id, "status": string(model.TripStatusActive)}).
		Suffix("RETURNING id, user_id, driver_id, start_latitude, start_longitude, end_latitude, end_longitude, price, status, started_at, finished_at, created_at, updated_at").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build finish trip query: %w", err)
	}

	row := executor.QueryRow(ctx, query, args...)

	var trip model.Trip
	var status string
	err = row.Scan(
		&trip.ID,
		&trip.UserID,
		&trip.DriverID,
		&trip.StartPoint.Latitude,
		&trip.StartPoint.Longitude,
		&trip.EndPoint.Latitude,
		&trip.EndPoint.Longitude,
		&trip.Price,
		&status,
		&trip.StartedAt,
		&trip.FinishedAt,
		&trip.CreatedAt,
		&trip.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Ноль затронутых строк: поездки либо нет, либо она уже завершена.
			// Требование задания: различить эти случаи через дополнительное чтение!
			existing, getErr := r.GetByID(ctx, id)
			if getErr != nil {
				return nil, getErr // вернет model.ErrNotFound, если поездки нет
			}
			if existing.Status == model.TripStatusCompleted {
				return nil, model.ErrTripCompleted
			}
			return nil, fmt.Errorf("unexpected status on finish: %s", existing.Status)
		}
		return nil, fmt.Errorf("scan finished trip: %w", err)
	}

	trip.Status = model.TripStatus(status)
	return &trip, nil
}
