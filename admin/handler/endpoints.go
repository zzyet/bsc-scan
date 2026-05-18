package handler

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

// EndpointRequest is the JSON body for create/update.
type EndpointRequest struct {
	URL                    string `json:"url"`
	RateLimitPerMinute     int    `json:"rateLimitPerMinute"`
	DailyLimit             int    `json:"dailyLimit"`
	MaxConsecutiveFailures int    `json:"maxConsecutiveFailures"`
	MaxTotalFailures       int    `json:"maxTotalFailures"`
	BackoffInitial         int    `json:"backoffInitial"`
	BackoffMax             int    `json:"backoffMax"`
	DailyResetHour         int    `json:"dailyResetHour"`
}

type epRow struct {
	ID                     int64      `json:"id"`
	URL                    string     `json:"url"`
	RateLimitPerMinute     int        `json:"rateLimitPerMinute"`
	DailyLimit             int        `json:"dailyLimit"`
	MaxConsecutiveFailures int        `json:"maxConsecutiveFailures"`
	MaxTotalFailures       int        `json:"maxTotalFailures"`
	BackoffInitial         int        `json:"backoffInitial"`
	BackoffMax             int        `json:"backoffMax"`
	DailyResetHour         int        `json:"dailyResetHour"`
	IsStopped              bool       `json:"isStopped"`
	DailyUsed              int        `json:"dailyUsed"`
	ConsecutiveFailures    int        `json:"consecutiveFailures"`
	TotalFailures          int        `json:"totalFailures"`
	Status                 string     `json:"status"`
	CreatedAt              *time.Time `json:"createdAt"`
	UpdatedAt              *time.Time `json:"updatedAt"`
}

func (h *Handler) ListEndpoints(w http.ResponseWriter, r *http.Request) {
	rows, err := h.db.Query(r.Context(),
		`SELECT id, url, rate_limit_per_minute, daily_limit, max_consecutive_failures,
		        max_total_failures, backoff_initial, backoff_max, daily_reset_hour,
		        is_stopped, daily_used, consecutive_failures, total_failures, status,
		        created_at, updated_at
		 FROM endpoints ORDER BY id`)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	defer rows.Close()

	var eps []epRow
	for rows.Next() {
		var ep epRow
		if err := rows.Scan(&ep.ID, &ep.URL, &ep.RateLimitPerMinute, &ep.DailyLimit,
			&ep.MaxConsecutiveFailures, &ep.MaxTotalFailures,
			&ep.BackoffInitial, &ep.BackoffMax, &ep.DailyResetHour,
			&ep.IsStopped, &ep.DailyUsed, &ep.ConsecutiveFailures, &ep.TotalFailures,
			&ep.Status, &ep.CreatedAt, &ep.UpdatedAt); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		eps = append(eps, ep)
	}
	writeJSON(w, 200, eps)
}

func (h *Handler) CreateEndpoint(w http.ResponseWriter, r *http.Request) {
	var req EndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}

	setDefaults(&req)

	var id int64
	err := h.db.QueryRow(r.Context(),
		`INSERT INTO endpoints (url, rate_limit_per_minute, daily_limit,
		 max_consecutive_failures, max_total_failures, backoff_initial,
		 backoff_max, daily_reset_hour)
		 VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING id`,
		req.URL, req.RateLimitPerMinute, req.DailyLimit,
		req.MaxConsecutiveFailures, req.MaxTotalFailures,
		req.BackoffInitial, req.BackoffMax, req.DailyResetHour,
	).Scan(&id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	writeJSON(w, 201, map[string]int64{"id": id})
}

func (h *Handler) GetEndpoint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid id")
		return
	}

	var ep epRow
	err = h.db.QueryRow(r.Context(),
		`SELECT id, url, rate_limit_per_minute, daily_limit, max_consecutive_failures,
		        max_total_failures, backoff_initial, backoff_max, daily_reset_hour,
		        is_stopped, daily_used, consecutive_failures, total_failures, status,
		        created_at, updated_at
		 FROM endpoints WHERE id=$1`, id,
	).Scan(&ep.ID, &ep.URL, &ep.RateLimitPerMinute, &ep.DailyLimit,
		&ep.MaxConsecutiveFailures, &ep.MaxTotalFailures,
		&ep.BackoffInitial, &ep.BackoffMax, &ep.DailyResetHour,
		&ep.IsStopped, &ep.DailyUsed, &ep.ConsecutiveFailures, &ep.TotalFailures,
		&ep.Status, &ep.CreatedAt, &ep.UpdatedAt)
	if err != nil {
		writeError(w, 404, "not found")
		return
	}
	writeJSON(w, 200, ep)
}

func (h *Handler) UpdateEndpoint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid id")
		return
	}

	var req EndpointRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, 400, "invalid JSON")
		return
	}
	setDefaults(&req)

	tag, err := h.db.Exec(r.Context(),
		`UPDATE endpoints SET url=$1, rate_limit_per_minute=$2, daily_limit=$3,
		 max_consecutive_failures=$4, max_total_failures=$5, backoff_initial=$6,
		 backoff_max=$7, daily_reset_hour=$8, updated_at=NOW()
		 WHERE id=$9`,
		req.URL, req.RateLimitPerMinute, req.DailyLimit,
		req.MaxConsecutiveFailures, req.MaxTotalFailures,
		req.BackoffInitial, req.BackoffMax, req.DailyResetHour, id,
	)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 404, "not found")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (h *Handler) DeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid id")
		return
	}

	tag, err := h.db.Exec(r.Context(), "DELETE FROM endpoints WHERE id=$1", id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 404, "not found")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "deleted"})
}

func (h *Handler) StopEndpoint(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, 400, "invalid id")
		return
	}

	tag, err := h.db.Exec(r.Context(),
		"UPDATE endpoints SET is_stopped=true, status='stopped', updated_at=NOW() WHERE id=$1", id)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if tag.RowsAffected() == 0 {
		writeError(w, 404, "not found")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "stopped"})
}

func setDefaults(req *EndpointRequest) {
	if req.RateLimitPerMinute == 0 {
		req.RateLimitPerMinute = 60
	}
	if req.MaxConsecutiveFailures == 0 {
		req.MaxConsecutiveFailures = 5
	}
	if req.BackoffInitial == 0 {
		req.BackoffInitial = 60
	}
	if req.BackoffMax == 0 {
		req.BackoffMax = 600
	}
}
