package create

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
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	var req api.PositionData
	if err := decoder.Decode(&req); err != nil {
		domainerror.WriteWithDetail(w, http.StatusBadRequest, "invalid_request", "Invalid request body", err.Error())
		return
	}

	input := toModelInput(tripID, req)
	pos, err := h.usecase.Execute(r.Context(), input)
	if err != nil {
		domainerror.Write(w, err)
		return
	}

	resp := toAPIPosition(pos)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(resp)
}
