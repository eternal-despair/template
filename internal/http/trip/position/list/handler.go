package list

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	api "github.com/eternal-despair/template/internal/generated"
	"github.com/eternal-despair/template/internal/http/domainerror"
)

type Handler struct {
	usecase usecase
}

func New(uc usecase) *Handler {
	return &Handler{usecase: uc}
}

func (h *Handler) Handle(w http.ResponseWriter, r *http.Request, tripID uuid.UUID) {
	positions, err := h.usecase.Execute(r.Context(), tripID)
	if err != nil {
		domainerror.Write(w, err)
		return
	}

	resp := api.TripPositionsResponse{
		Positions: toAPIPositions(positions),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
