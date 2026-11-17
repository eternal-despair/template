package trip

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/eternal-despair/template/internal/domain/trip"
	"github.com/eternal-despair/template/internal/model"
)

const pgErrUniqueViolation = "23505"

func (r *Repository) Create(ctx context.Context, tr *model.Trip) error {
	exec := r.getExecutor(ctx)

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
			tr.ID,
			tr.UserID,
			tr.DriverID,
			tr.StartPoint.Latitude,
			tr.StartPoint.Longitude,
			tr.EndPoint.Latitude,
			tr.EndPoint.Longitude,
			tr.Price,
			string(tr.Status),
			tr.StartedAt,
			tr.CreatedAt,
			tr.UpdatedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip query: %w", err)
	}

	_, err = exec.Exec(ctx, query, args...)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrUniqueViolation {
			return trip.ErrDriverBusy
		}
		return fmt.Errorf("exec insert trip: %w", err)
	}

	return nil
}

func (r *Repository) CreateStatusHistory(ctx context.Context, history *model.TripStatusHistory) error {
	exec := r.getExecutor(ctx)

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
			history.ToStatus,
			history.Reason,
			history.ChangedAt,
		).
		ToSql()
	if err != nil {
		return fmt.Errorf("build insert status history query: %w", err)
	}

	_, err = exec.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("exec insert status history: %w", err)
	}

	return nil
}

func (r *Repository) Finish(ctx context.Context, id uuid.UUID, finishedAt time.Time) (*model.Trip, error) {
	exec := r.getExecutor(ctx)

	query, args, err := r.builder.
		Update("trips").
		Set("status", string(trip.StatusCompleted)).
		Set("finished_at", finishedAt).
		Set("updated_at", finishedAt).
		Where(sq.Eq{
			"id":     id,
			"status": string(trip.StatusActive),
		}).
		Suffix("RETURNING " + strings.Join(tripColumns, ", ")).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build finish trip query: %w", err)
	}

	var d dbTrip
	row := exec.QueryRow(ctx, query, args...)
	err = row.Scan(
		&d.ID,
		&d.UserID,
		&d.DriverID,
		&d.StartLatitude,
		&d.StartLongitude,
		&d.EndLatitude,
		&d.EndLongitude,
		&d.Price,
		&d.Status,
		&d.StartedAt,
		&d.FinishedAt,
		&d.LastPositionAt,
		&d.CreatedAt,
		&d.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			existing, getErr := r.GetByID(ctx, id)
			if getErr != nil {
				return nil, getErr
			}
			if existing.Status == trip.StatusCompleted {
				return nil, trip.ErrTripCompleted
			}
			return nil, trip.ErrTripNotFound
		}
		return nil, fmt.Errorf("scan finished trip: %w", err)
	}

	return d.toModel(), nil
}

func (r *Repository) SavePosition(ctx context.Context, pos *model.CreatePositionInput) (*model.TripPosition, error) {
	exec := r.getExecutor(ctx)

	const insertPosSQL = `
		INSERT INTO trip_positions (trip_id, latitude, longitude, recorded_at)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at
	`

	var result model.TripPosition
	result.TripID = pos.TripID
	result.Coordinates = pos.Coordinates
	result.RecordedAt = pos.RecordedAt

	err := exec.QueryRow(ctx, insertPosSQL, pos.TripID, pos.Coordinates.Latitude, pos.Coordinates.Longitude, pos.RecordedAt).
		Scan(&result.ID, &result.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert trip position: %w", err)
	}

	const updateTripSQL = `
		UPDATE trips
		SET last_position_at = CASE
			WHEN last_position_at IS NULL OR $2 > last_position_at THEN $2
			ELSE last_position_at
		END,
		updated_at = now()
		WHERE id = $1
	`

	_, err = exec.Exec(ctx, updateTripSQL, pos.TripID, pos.RecordedAt)
	if err != nil {
		return nil, fmt.Errorf("update trip last_position_at: %w", err)
	}

	return &result, nil
}
