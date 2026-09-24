// Package httpapi es el adaptador de entrada REST (net/http).
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"leancore-fintech/backend/internal/application/ports"
	"leancore-fintech/backend/internal/domain"
)

const (
	idempotencyHeader = "Idempotency-Key"
	maxBodyBytes      = 1 << 16
)

// Handler expone ports.CreditService por HTTP.
type Handler struct {
	svc ports.CreditService
	log *slog.Logger
}

func NewHandler(svc ports.CreditService, log *slog.Logger) http.Handler {
	h := &Handler{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("POST /api/credits", h.createCredit)
	mux.HandleFunc("GET /api/credits", h.listCredits)
	mux.HandleFunc("GET /api/credits/{id}", h.getCredit)
	mux.HandleFunc("GET /api/credits/{id}/entries", h.listEntries)
	mux.HandleFunc("GET /api/credits/{id}/preview", h.preview)
	mux.HandleFunc("POST /api/credits/{id}/purchases", h.purchase)
	mux.HandleFunc("POST /api/credits/{id}/payments", h.payment)
	mux.HandleFunc("POST /api/credits/{id}/interest-liquidations", h.liquidate)
	return mux
}

func (h *Handler) createCredit(w http.ResponseWriter, r *http.Request) {
	key, err := idempotencyKey(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	var req createCreditRequest
	if err := decode(r, w, &req); err != nil {
		h.fail(w, err)
		return
	}
	limit, err := parseInteger(req.LimitCents, "limitCents")
	if err != nil {
		h.fail(w, err)
		return
	}
	rate, err := parseInteger(req.RateEABps, "rateEaBps")
	if err != nil {
		h.fail(w, err)
		return
	}
	amount, err := parseInteger(req.Disbursement.AmountCents, "amountCents")
	if err != nil {
		h.fail(w, err)
		return
	}
	date, err := parseDate(req.Disbursement.Date, "date")
	if err != nil {
		h.fail(w, err)
		return
	}
	if rate < 0 || rate > domain.MaxRateEABps {
		rate = -1 // fuera de rango: lo rechaza el dominio sin desbordar int
	}
	res, err := h.svc.OpenCredit(r.Context(), ports.OpenCreditCommand{
		IdempotencyKey: key, Holder: req.Holder, LimitCents: domain.Money(limit), RateEABps: int(rate),
		AmountCents: domain.Money(amount), DisbursementDate: date, Description: req.Disbursement.Description,
	})
	if err != nil {
		h.fail(w, err)
		return
	}
	cl := res.Credit
	writeJSON(w, created(res.Replayed), createCreditResponse{
		ID: cl.ID, AccountNumber: cl.AccountNumber, Holder: cl.Holder, LimitCents: int64(cl.Limit),
		RateEABps: cl.RateEABps, Disbursement: toEntry(res.Entry),
	})
}

func (h *Handler) listCredits(w http.ResponseWriter, r *http.Request) {
	lines, err := h.svc.ListCredits(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	out := make([]creditSummary, 0, len(lines))
	for _, cl := range lines {
		out = append(out, toSummary(cl))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) getCredit(w http.ResponseWriter, r *http.Request) {
	id, err := creditID(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	v, err := h.svc.GetCredit(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDetail(v))
}

func (h *Handler) listEntries(w http.ResponseWriter, r *http.Request) {
	id, err := creditID(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	v, err := h.svc.ListEntries(r.Context(), id)
	if err != nil {
		h.fail(w, err)
		return
	}
	out := entriesResponse{Entries: make([]entryResponse, 0, len(v.Entries)),
		TotalDebitCents: int64(v.TotalDebit), TotalCreditCents: int64(v.TotalCredit)}
	for _, e := range v.Entries {
		out.Entries = append(out.Entries, toEntry(e))
	}
	writeJSON(w, http.StatusOK, out)
}

func (h *Handler) preview(w http.ResponseWriter, r *http.Request) {
	id, err := creditID(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	raw := r.URL.Query().Get("date")
	if raw == "" {
		h.fail(w, &apiError{status: 400, Code: "INVALID_DATE", Message: "Indica la fecha (date=AAAA-MM-DD).",
			Details: map[string]any{"field": "date"}})
		return
	}
	date, err := parseDate(raw, "date")
	if err != nil {
		h.fail(w, err)
		return
	}
	p, err := h.svc.PreviewAt(r.Context(), id, date)
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toPreview(p))
}

func (h *Handler) purchase(w http.ResponseWriter, r *http.Request) {
	h.movement(w, r, h.svc.RegisterPurchase)
}

func (h *Handler) payment(w http.ResponseWriter, r *http.Request) {
	h.movement(w, r, h.svc.RegisterPayment)
}

type movementFunc func(ctx context.Context, cmd ports.MovementCommand) (ports.MovementResult, error)

func (h *Handler) movement(w http.ResponseWriter, r *http.Request, run movementFunc) {
	id, err := creditID(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	var req movementRequest
	if err := decode(r, w, &req); err != nil {
		h.fail(w, err)
		return
	}
	amount, err := parseInteger(req.AmountCents, "amountCents")
	if err != nil {
		h.fail(w, err)
		return
	}
	date, err := parseDate(req.Date, "date")
	if err != nil {
		h.fail(w, err)
		return
	}
	res, err := run(r.Context(), ports.MovementCommand{CreditID: id, IdempotencyKey: key,
		AmountCents: domain.Money(amount), Date: date, Description: req.Description})
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, created(res.Replayed), movementResponse{Entry: toEntry(res.Entry)})
}

func (h *Handler) liquidate(w http.ResponseWriter, r *http.Request) {
	id, err := creditID(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	key, err := idempotencyKey(r)
	if err != nil {
		h.fail(w, err)
		return
	}
	var req liquidateRequest
	if err := decode(r, w, &req); err != nil {
		h.fail(w, err)
		return
	}
	date, err := parseDate(req.Date, "date")
	if err != nil {
		h.fail(w, err)
		return
	}
	res, err := h.svc.LiquidateInterest(r.Context(), ports.LiquidateCommand{CreditID: id, IdempotencyKey: key, Date: date})
	if err != nil {
		h.fail(w, err)
		return
	}
	writeJSON(w, created(res.Replayed), movementResponse{Entry: toEntry(res.Entry)})
}

// --- helpers ---------------------------------------------------------------

func created(replayed bool) int {
	if replayed {
		return http.StatusOK
	}
	return http.StatusCreated
}

func creditID(r *http.Request) (string, error) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		return "", ports.ErrNotFound
	}
	return id.String(), nil
}

func idempotencyKey(r *http.Request) (string, error) {
	raw := strings.TrimSpace(r.Header.Get(idempotencyHeader))
	if raw == "" {
		return "", ports.ErrIdempotencyKeyRequired
	}
	k, err := uuid.Parse(raw)
	if err != nil {
		return "", &apiError{status: 400, Code: "INVALID_IDEMPOTENCY_KEY",
			Message: "La llave de idempotencia debe ser un UUID."}
	}
	return k.String(), nil
}

func decode(r *http.Request, w http.ResponseWriter, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.UseNumber()
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return &apiError{status: 400, Code: "MALFORMED_REQUEST", Message: "El cuerpo de la solicitud no es JSON válido.",
			Details: map[string]any{"reason": err.Error()}}
	}
	return nil
}

type apiError struct {
	status  int
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Details map[string]any `json:"details,omitempty"`
}

func (e *apiError) Error() string { return fmt.Sprintf("%d %s: %s", e.status, e.Code, e.Message) }

func (h *Handler) fail(w http.ResponseWriter, err error) {
	var ae *apiError
	var de *domain.Error
	switch {
	case errors.As(err, &ae):
	case errors.As(err, &de):
		ae = &apiError{status: http.StatusUnprocessableEntity, Code: string(de.Code), Message: de.Message, Details: de.Details}
	case errors.Is(err, ports.ErrNotFound):
		ae = &apiError{status: http.StatusNotFound, Code: "NOT_FOUND", Message: "El crédito no fue encontrado."}
	case errors.Is(err, ports.ErrIdempotencyKeyRequired):
		ae = &apiError{status: http.StatusBadRequest, Code: "IDEMPOTENCY_KEY_REQUIRED",
			Message: "El header Idempotency-Key es obligatorio."}
	case errors.Is(err, ports.ErrIdempotencyKeyReused):
		ae = &apiError{status: http.StatusUnprocessableEntity, Code: "IDEMPOTENCY_KEY_REUSED",
			Message: "Esta operación ya fue procesada con otros datos."}
	default:
		h.log.Error("error interno", "err", err)
		ae = &apiError{status: http.StatusInternalServerError, Code: "INTERNAL", Message: "Error interno."}
	}
	writeJSON(w, ae.status, ae)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
