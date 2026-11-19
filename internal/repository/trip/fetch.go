package trip

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/eternal-despair/template/internal/domain/trip"
	"github.com/eternal-despair/template/internal/model"
)

var tripColumns = []string{
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
	"last_position_at",
	"created_at",
	"updated_at",
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	exec := r.getExecutor(ctx)

	query, args, err := r.builder.
		Select(tripColumns...).
		From("trips").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select trip query: %w", err)
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
			return nil, trip.ErrTripNotFound
		}
		return nil, fmt.Errorf("scan trip: %w", err)
	}

	return d.toModel(), nil
}

func (r *Repository) ListPositions(ctx context.Context, tripID uuid.UUID) ([]*model.TripPosition, error) {
	exec := r.getExecutor(ctx)

	const query = `
		SELECT id, trip_id, latitude, longitude, recorded_at, created_at
		FROM trip_positions
		WHERE trip_id = $1
		ORDER BY recorded_at ASC, id ASC
	`

	rows, err := exec.Query(ctx, query, tripID)
	if err != nil {
		return nil, fmt.Errorf("query positions: %w", err)
	}
	defer rows.Close()

	var positions []*model.TripPosition
	for rows.Next() {
		var p model.TripPosition
		if err := rows.Scan(
			&p.ID,
			&p.TripID,
			&p.Coordinates.Latitude,
			&p.Coordinates.Longitude,
			&p.RecordedAt,
			&p.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		positions = append(positions, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows positions error: %w", err)
	}

	if positions == nil {
		positions = make([]*model.TripPosition, 0)
	}

	return positions, nil
}

func (r *Repository) GetStaleTrips(ctx context.Context, staleBefore time.Time, limit int) ([]*model.Trip, error) {
	exec := r.getExecutor(ctx)

	var query string
	var args []any

	if limit > 0 {
		query = `
			SELECT id, user_id, driver_id, start_latitude, start_longitude, end_latitude, end_longitude,
			       price, status, started_at, finished_at, last_position_at, created_at, updated_at
			FROM trips
			WHERE status = 'active' AND COALESCE(last_position_at, started_at) < $1
			ORDER BY COALESCE(last_position_at, started_at) ASC
			LIMIT $2
		`
		args = []any{staleBefore, limit}
	} else {
		query = `
			SELECT id, user_id, driver_id, start_latitude, start_longitude, end_latitude, end_longitude,
			       price, status, started_at, finished_at, last_position_at, created_at, updated_at
			FROM trips
			WHERE status = 'active' AND COALESCE(last_position_at, started_at) < $1
			ORDER BY COALESCE(last_position_at, started_at) ASC
		`
		args = []any{staleBefore}
	}

	rows, err := exec.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query stale trips: %w", err)
	}
	defer rows.Close()

	var trips []*model.Trip
	for rows.Next() {
		var d dbTrip
		if err := rows.Scan(
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
		); err != nil {
			return nil, fmt.Errorf("scan stale trip: %w", err)
		}
		trips = append(trips, d.toModel())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows stale trips error: %w", err)
	}

	return trips, nil
}
