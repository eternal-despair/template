package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	api "github.com/eternal-despair/template/internal/generated"
	"github.com/eternal-despair/template/internal/model"
	"github.com/eternal-despair/template/internal/service"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Handler struct {
	service service.TripService
	pool    *pgxpool.Pool
}

func NewHandler(service service.TripService, pool *pgxpool.Pool) *Handler {
	return &Handler{
		service: service,
		pool:    pool,
	}
}

// POST /api/v1/trips
func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields() // Запрещаем неизвестные поля согласно контракту

	var req api.TripData
	if err := decoder.Decode(&req); err != nil {
		h.writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request body", err.Error())
		return
	}

	trip, err := h.service.CreateTrip(r.Context(), service.CreateTripInput{
		UserID:   req.UserId,
		DriverID: req.DriverId,
		StartPoint: model.Coordinates{
			Latitude:  req.StartPoint.Latitude,
			Longitude: req.StartPoint.Longitude,
		},
		EndPoint: model.Coordinates{
			Latitude:  req.EndPoint.Latitude,
			Longitude: req.EndPoint.Longitude,
		},
		Price: req.Price,
	})
	if err != nil {
		if errors.Is(err, service.ErrInvalidRequest) {
			h.writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Validation failed", err.Error())
			return
		}
		if errors.Is(err, model.ErrDriverBusy) {
			h.writeProblem(w, r, http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
			return
		}
		h.writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal error", "Internal server error")
		return
	}

	resp := toAPITrip(trip)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Location", fmt.Sprintf("/api/v1/trips/%s", trip.ID))
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}

// GET /api/v1/trips/{tripId}
func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	trip, err := h.service.GetTrip(r.Context(), tripId)
	if err != nil {
		if errors.Is(err, service.ErrInvalidRequest) {
			h.writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid trip id", err.Error())
			return
		}
		if errors.Is(err, model.ErrNotFound) {
			h.writeProblem(w, r, http.StatusNotFound, "trip_not_found", "Trip not found", "Trip with specified ID was not found")
			return
		}
		h.writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal error", "Internal server error")
		return
	}

	resp := toAPITrip(trip)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// POST /api/v1/trips/{tripId}/finish
func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	trip, err := h.service.FinishTrip(r.Context(), tripId)
	if err != nil {
		if errors.Is(err, service.ErrInvalidRequest) {
			h.writeProblem(w, r, http.StatusBadRequest, "invalid_request", "Invalid trip id", err.Error())
			return
		}
		if errors.Is(err, model.ErrNotFound) {
			h.writeProblem(w, r, http.StatusNotFound, "trip_not_found", "Trip not found", "Trip with specified ID was not found")
			return
		}
		if errors.Is(err, model.ErrTripCompleted) {
			h.writeProblem(w, r, http.StatusConflict, "trip_completed", "Trip completed", "Trip is already completed")
			return
		}
		h.writeProblem(w, r, http.StatusInternalServerError, "internal_error", "Internal error", "Internal server error")
		return
	}

	resp := toAPITrip(trip)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}

// GET /health (Liveness — не ходит в БД)
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(api.HealthResponse{
		Status: api.Ok,
	})
}

// GET /ready (Readiness — проверяет БД)
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")
	if err := h.pool.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(api.HealthResponse{
			Status: api.Unavailable,
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(api.HealthResponse{
		Status: api.Ok,
	})
}

// writeProblem отдает ошибку по RFC 9457 в формате application/problem+json
func (h *Handler) writeProblem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)

	uri := fmt.Sprintf("https://tripgo.example/problems/%s", toKebabCase(code))
	path := r.URL.Path

	problem := api.Problem{
		Type:     uri,
		Title:    title,
		Status:   int32(status),
		Detail:   &detail,
		Instance: &path,
		Code:     code,
	}

	_ = json.NewEncoder(w).Encode(problem)
}

func toAPITrip(trip *model.Trip) api.Trip {
	return api.Trip{
		Id:       trip.ID,
		UserId:   trip.UserID,
		DriverId: trip.DriverID,
		StartPoint: api.Coordinates{
			Latitude:  trip.StartPoint.Latitude,
			Longitude: trip.StartPoint.Longitude,
		},
		EndPoint: api.Coordinates{
			Latitude:  trip.EndPoint.Latitude,
			Longitude: trip.EndPoint.Longitude,
		},
		Price:          trip.Price,
		Status:         api.TripStatus(trip.Status),
		StartedAt:      trip.StartedAt,
		FinishedAt:     trip.FinishedAt,
		LastPositionAt: trip.LastPositionAt,
	}
}

func toKebabCase(s string) string {
	res := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		if s[i] == '_' {
			res = append(res, '-')
		} else {
			res = append(res, s[i])
		}
	}
	return string(res)
}
