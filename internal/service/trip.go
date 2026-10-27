package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/eternal-despair/template/internal/model"
	"github.com/eternal-despair/template/internal/repository"
	"github.com/eternal-despair/template/internal/txmanager"
	"github.com/google/uuid"
)

var (
	ErrInvalidRequest = errors.New("invalid request")
)

type TripService interface {
	CreateTrip(ctx context.Context, input CreateTripInput) (*model.Trip, error)
	GetTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	FinishTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error)
}

type CreateTripInput struct {
	UserID     uuid.UUID
	DriverID   uuid.UUID
	StartPoint model.Coordinates
	EndPoint   model.Coordinates
	Price      int64
}

type tripService struct {
	repo      repository.TripRepository
	txManager txmanager.TxManager
}

func NewTripService(repo repository.TripRepository, txManager txmanager.TxManager) TripService {
	return &tripService{
		repo:      repo,
		txManager: txManager,
	}
}

func (s *tripService) CreateTrip(ctx context.Context, input CreateTripInput) (*model.Trip, error) {
	// Валидация входных данных
	if input.UserID == uuid.Nil || input.DriverID == uuid.Nil {
		return nil, fmt.Errorf("%w: user_id and driver_id cannot be empty", ErrInvalidRequest)
	}
	if input.StartPoint.Latitude < -90 || input.StartPoint.Latitude > 90 ||
		input.StartPoint.Longitude < -180 || input.StartPoint.Longitude > 180 {
		return nil, fmt.Errorf("%w: invalid start coordinates", ErrInvalidRequest)
	}
	if input.EndPoint.Latitude < -90 || input.EndPoint.Latitude > 90 ||
		input.EndPoint.Longitude < -180 || input.EndPoint.Longitude > 180 {
		return nil, fmt.Errorf("%w: invalid end coordinates", ErrInvalidRequest)
	}
	if input.Price < 0 {
		return nil, fmt.Errorf("%w: price cannot be negative", ErrInvalidRequest)
	}

	now := time.Now().UTC()
	trip := &model.Trip{
		ID:         uuid.New(),
		UserID:     input.UserID,
		DriverID:   input.DriverID,
		StartPoint: input.StartPoint,
		EndPoint:   input.EndPoint,
		Price:      input.Price,
		Status:     model.TripStatusActive,
		StartedAt:  now,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	reason := "trip created"
	history := &model.TripStatusHistory{
		TripID:     trip.ID,
		FromStatus: nil,
		ToStatus:   model.TripStatusActive,
		Reason:     &reason,
		ChangedAt:  now,
	}

	// Атомарная запись в trips и trip_status_history через TxManager
	err := s.txManager.Do(ctx, func(txCtx context.Context) error {
		if err := s.repo.Create(txCtx, trip); err != nil {
			return err
		}
		if err := s.repo.CreateStatusHistory(txCtx, history); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return trip, nil
}

func (s *tripService) GetTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("%w: trip id cannot be empty", ErrInvalidRequest)
	}
	return s.repo.GetByID(ctx, id)
}

func (s *tripService) FinishTrip(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("%w: trip id cannot be empty", ErrInvalidRequest)
	}

	var finishedTrip *model.Trip
	now := time.Now().UTC()
	reason := "trip finished"

	// Атомарное обновление поездки и добавление записи в историю
	err := s.txManager.Do(ctx, func(txCtx context.Context) error {
		trip, err := s.repo.Finish(txCtx, id, now)
		if err != nil {
			return err
		}
		finishedTrip = trip

		history := &model.TripStatusHistory{
			TripID:     id,
			FromStatus: ptrString(string(model.TripStatusActive)),
			ToStatus:   model.TripStatusCompleted,
			Reason:     &reason,
			ChangedAt:  now,
		}

		return s.repo.CreateStatusHistory(txCtx, history)
	})
	if err != nil {
		return nil, err
	}

	return finishedTrip, nil
}

func ptrString(s string) *string {
	return &s
}
