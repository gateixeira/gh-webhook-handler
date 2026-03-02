package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gateixeira/gh-webhook-handler/internal/circuitbreaker"
	"github.com/gateixeira/gh-webhook-handler/internal/config"
	"github.com/gateixeira/gh-webhook-handler/internal/store"
)

// API provides admin REST endpoints for inspecting deliveries and routes.
type API struct {
	store   store.Store
	cfg     *config.Config
	breaker *circuitbreaker.Breaker
}

// NewAPI creates an admin API handler.
func NewAPI(s store.Store, cfg *config.Config, breaker *circuitbreaker.Breaker) *API {
	return &API{store: s, cfg: cfg, breaker: breaker}
}

// RegisterRoutes attaches the admin endpoints to the given ServeMux.
func (a *API) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/deliveries", a.listDeliveries)
	mux.HandleFunc("GET /api/deliveries/{id}", a.getDelivery)
	mux.HandleFunc("POST /api/deliveries/{id}/retrigger", a.retriggerDelivery)
	mux.HandleFunc("GET /api/routes", a.listRoutes)
	mux.HandleFunc("GET /api/circuits", a.listCircuits)
	mux.HandleFunc("POST /api/circuits/reset", a.resetCircuit)
}

func (a *API) listDeliveries(w http.ResponseWriter, r *http.Request) {
	filter := store.ListFilter{
		RouteName: r.URL.Query().Get("route"),
		EventType: r.URL.Query().Get("event"),
		Status:    r.URL.Query().Get("status"),
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Limit = n
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			filter.Offset = n
		}
	}

	deliveries, err := a.store.List(filter)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, deliveries)
}

func (a *API) getDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := a.store.Get(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "delivery not found")
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *API) retriggerDelivery(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	d, err := a.store.Get(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "delivery not found")
		return
	}

	now := time.Now()
	d.Status = "failed"
	d.NextRetryAt = &now
	d.UpdatedAt = now

	if err := a.store.Update(d); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (a *API) listRoutes(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.cfg.Routes())
}

func (a *API) listCircuits(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.breaker.List())
}

func (a *API) resetCircuit(w http.ResponseWriter, r *http.Request) {
	url := r.URL.Query().Get("url")
	if url == "" {
		writeError(w, http.StatusBadRequest, "url query parameter is required")
		return
	}
	a.breaker.Reset(url)
	writeJSON(w, http.StatusOK, map[string]string{"status": "reset", "url": url})
}

// --- helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
