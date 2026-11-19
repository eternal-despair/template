package model

import (
	"time"

	"github.com/google/uuid"

	"github.com/eternal-despair/template/internal/domain/trip"
)

type (
	Trip        = trip.Trip
	Coordinates = trip.Coordinates
	TripStatus  = trip.Status
)

type CreateTripInput struct {
	UserID     uuid.UUID
	DriverID   uuid.UUID
	StartPoint Coordinates
	EndPoint   Coordinates
	Price      int64
}

type TripStatusHistory struct {
	ID         int64
	TripID     uuid.UUID
	FromStatus *string
	ToStatus   string
	Reason     *string
	ChangedAt  time.Time
}

type TripPosition struct {
	ID          int64
	TripID      uuid.UUID
	Coordinates Coordinates
	RecordedAt  time.Time
	CreatedAt   time.Time
}

type CreatePositionInput struct {
	TripID      uuid.UUID
	Coordinates Coordinates
	RecordedAt  time.Time
}
