package httpapi

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"leancore-fintech/backend/internal/adapters/out/ids"
	"leancore-fintech/backend/internal/adapters/out/memory"
	"leancore-fintech/backend/internal/application/usecases"
)

type fixedClock struct{}

func (fixedClock) Today() time.Time { return time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC) }

func newServer(t *testing.T) *httptest.Server {
	t.Helper()
	svc := usecases.NewService(memory.NewRepository(), fixedClock{}, ids.Generator{})
	srv := httptest.NewServer(NewHandler(svc, slog.New(slog.NewTextHandler(io.Discard, nil))))
	t.Cleanup(srv.Close)
	return srv
}

type resp struct {
	status int
	body   string
}

func (r resp) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal([]byte(r.body), v); err != nil {
		t.Fatalf("json %q: %v", r.body, err)
	}
}

func (r resp) code(t *testing.T) string {
	var e struct{ Code string }
	r.json(t, &e)
	return e.Code
}

func do(t *testing.T, srv *httptest.Server, method, path, key, body string) resp {
	t.Helper()
	req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return resp{res.StatusCode, string(b)}
}

const openBody = `{"holder":"María Fernanda Ruiz","limitCents":1000000000,"rateEaBps":2400,
	"disbursement":{"amountCents":500000000,"date":"2026-06-01","description":""}}`

func open(t *testing.T, srv *httptest.Server) string {
	t.Helper()
	r := do(t, srv, "POST", "/api/credits", uuid.NewString(), openBody)
	if r.status != 201 {
		t.Fatalf("open: %d %s", r.status, r.body)
	}
	var c createCreditResponse
	r.json(t, &c)
	return c.ID
}

func TestCrearCredito(t *testing.T) {
	srv := newServer(t)
	key := uuid.NewString()
	r := do(t, srv, "POST", "/api/credits", key, openBody)
	if r.status != 201 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	var c createCreditResponse
	r.json(t, &c)
	if c.AccountNumber == "" || c.LimitCents != 1_000_000_000 || c.Disbursement.Type != "DESEMBOLSO" ||
		c.Disbursement.AmountCents != 500_000_000 || c.Disbursement.Description != "Desembolso inicial del crédito" {
		t.Fatalf("%+v", c)
	}
	// Reintento idempotente: mismo cuerpo, 200.
	again := do(t, srv, "POST", "/api/credits", key, openBody)
	if again.status != 200 || again.body != r.body {
		t.Fatalf("reintento: %d %s", again.status, again.body)
	}
	// Misma llave con otro contenido: 422.
	other := do(t, srv, "POST", "/api/credits", key, strings.Replace(openBody, "500000000", "1000", 1))
	if other.status != 422 || other.code(t) != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("%d %s", other.status, other.body)
	}
}

func TestCrearCreditoErrores(t *testing.T) {
	srv := newServer(t)
	tests := []struct {
		name, key, body string
		status          int
		code            string
	}{
		{"sin llave", "", openBody, 400, "IDEMPOTENCY_KEY_REQUIRED"},
		{"llave no uuid", "abc", openBody, 400, "INVALID_IDEMPOTENCY_KEY"},
		{"json inválido", uuid.NewString(), `{`, 400, "MALFORMED_REQUEST"},
		{"campo desconocido", uuid.NewString(), `{"foo":1}`, 400, "MALFORMED_REQUEST"},
		{"monto decimal", uuid.NewString(), strings.Replace(openBody, "500000000", "1500.5", 1), 400, "INVALID_AMOUNT"},
		{"monto exponente", uuid.NewString(), strings.Replace(openBody, "500000000", "1e3", 1), 400, "INVALID_AMOUNT"},
		{"fecha inválida", uuid.NewString(), strings.Replace(openBody, "2026-06-01", "01/06/2026", 1), 400, "INVALID_DATE"},
		{"supera cupo", uuid.NewString(), strings.Replace(openBody, "500000000", "1000000001", 1), 422, "DISBURSEMENT_EXCEEDS_LIMIT"},
		{"sin titular", uuid.NewString(), strings.Replace(openBody, "María Fernanda Ruiz", " ", 1), 422, "VALIDATION"},
		{"tasa negativa", uuid.NewString(), strings.Replace(openBody, "2400", "-5", 1), 422, "VALIDATION"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := do(t, srv, "POST", "/api/credits", tt.key, tt.body)
			if r.status != tt.status || r.code(t) != tt.code {
				t.Fatalf("%d %s", r.status, r.body)
			}
		})
	}
}

func TestListadoYConsulta(t *testing.T) {
	srv := newServer(t)
	empty := do(t, srv, "GET", "/api/credits", "", "")
	if empty.status != 200 || strings.TrimSpace(empty.body) != "[]" {
		t.Fatalf("%d %s", empty.status, empty.body)
	}
	id := open(t, srv)
	var list []creditSummary
	do(t, srv, "GET", "/api/credits", "", "").json(t, &list)
	if len(list) != 1 || list[0].Status != "AL_DIA" || list[0].BalanceCents != 500_000_000 || list[0].UsedBps != 5000 {
		t.Fatalf("%+v", list)
	}

	r := do(t, srv, "GET", "/api/credits/"+id, "", "")
	var c creditDetail
	r.json(t, &c)
	// Reloj fijo 2026-07-01: 30 días devengados sobre 500000000, sin escribir.
	if r.status != 200 || c.ProjectionDate != "2026-07-01" || c.AccruedInterestCents == 0 ||
		c.ProjectedBalanceCents != 500_000_000+c.AccruedInterestCents || c.InterestDueCents != 0 {
		t.Fatalf("%d %+v", r.status, c)
	}

	for _, path := range []string{"/api/credits/" + uuid.NewString(), "/api/credits/no-uuid"} {
		if r := do(t, srv, "GET", path, "", ""); r.status != 404 || r.code(t) != "NOT_FOUND" {
			t.Fatalf("%s: %d %s", path, r.status, r.body)
		}
	}
}

func TestConsumo(t *testing.T) {
	srv := newServer(t)
	id := open(t, srv)
	r := do(t, srv, "POST", "/api/credits/"+id+"/purchases", uuid.NewString(),
		`{"amountCents":35000000,"date":"2026-06-05","description":"Compra supermercado"}`)
	var m movementResponse
	r.json(t, &m)
	if r.status != 201 || m.Entry.Type != "CONSUMO" || m.Entry.BalanceAfterCents != 535_000_000 {
		t.Fatalf("%d %s", r.status, r.body)
	}
	over := do(t, srv, "POST", "/api/credits/"+id+"/purchases", uuid.NewString(),
		`{"amountCents":500000000,"date":"2026-06-05"}`)
	if over.status != 422 || over.code(t) != "INSUFFICIENT_AVAILABLE" || !strings.Contains(over.body, `"availableCents":465000000`) {
		t.Fatalf("%d %s", over.status, over.body)
	}
	before := do(t, srv, "POST", "/api/credits/"+id+"/purchases", uuid.NewString(),
		`{"amountCents":1,"date":"2026-06-01"}`)
	if before.status != 422 || before.code(t) != "DATE_BEFORE_LAST_ENTRY" {
		t.Fatalf("%d %s", before.status, before.body)
	}
	missing := do(t, srv, "POST", "/api/credits/"+uuid.NewString()+"/purchases", uuid.NewString(),
		`{"amountCents":1,"date":"2026-06-05"}`)
	if missing.status != 404 {
		t.Fatalf("%d %s", missing.status, missing.body)
	}
}

func TestPagoIdempotenteYDetalle(t *testing.T) {
	srv := newServer(t)
	id := open(t, srv)
	key := uuid.NewString()
	body := `{"amountCents":100000000,"date":"2026-07-01"}`
	first := do(t, srv, "POST", "/api/credits/"+id+"/payments", key, body)
	if first.status != 201 {
		t.Fatalf("%d %s", first.status, first.body)
	}
	var m movementResponse
	first.json(t, &m)
	if m.Entry.Type != "PAGO" || m.Entry.ToInterestCents == nil || *m.Entry.ToInterestCents == 0 ||
		*m.Entry.ToInterestCents+*m.Entry.ToCapitalCents != 100_000_000 || len(m.Entry.Allocations) != 1 {
		t.Fatalf("%s", first.body)
	}
	again := do(t, srv, "POST", "/api/credits/"+id+"/payments", key, body)
	if again.status != 200 || again.body != first.body {
		t.Fatalf("reintento: %d %s", again.status, again.body)
	}
	reused := do(t, srv, "POST", "/api/credits/"+id+"/payments", key, `{"amountCents":1,"date":"2026-07-01"}`)
	if reused.status != 422 || reused.code(t) != "IDEMPOTENCY_KEY_REUSED" {
		t.Fatalf("%d %s", reused.status, reused.body)
	}
	exceeds := do(t, srv, "POST", "/api/credits/"+id+"/payments", uuid.NewString(), `{"amountCents":900000000,"date":"2026-07-01"}`)
	if exceeds.status != 422 || exceeds.code(t) != "PAYMENT_EXCEEDS_BALANCE" {
		t.Fatalf("%d %s", exceeds.status, exceeds.body)
	}
	noKey := do(t, srv, "POST", "/api/credits/"+id+"/payments", "", body)
	if noKey.status != 400 || noKey.code(t) != "IDEMPOTENCY_KEY_REQUIRED" {
		t.Fatalf("%d %s", noKey.status, noKey.body)
	}
	decimal := do(t, srv, "POST", "/api/credits/"+id+"/payments", uuid.NewString(), `{"amountCents":1500.5,"date":"2026-07-01"}`)
	if decimal.status != 400 || decimal.code(t) != "INVALID_AMOUNT" {
		t.Fatalf("%d %s", decimal.status, decimal.body)
	}

	// Historial: PAGO, INTERES automático, DESEMBOLSO (más reciente primero) y totales.
	var h entriesResponse
	do(t, srv, "GET", "/api/credits/"+id+"/entries", "", "").json(t, &h)
	if len(h.Entries) != 3 || h.Entries[0].Type != "PAGO" || h.Entries[1].Type != "INTERES" || h.Entries[2].Type != "DESEMBOLSO" {
		t.Fatalf("%+v", h.Entries)
	}
	if h.TotalCreditCents != 100_000_000 || h.TotalDebitCents != 500_000_000+h.Entries[1].AmountCents {
		t.Fatalf("totales %+v", h)
	}
}

func TestVistaPreviaYLiquidacion(t *testing.T) {
	srv := newServer(t)
	id := open(t, srv)
	var p previewResponse
	r := do(t, srv, "GET", "/api/credits/"+id+"/preview?date=2026-07-01", "", "")
	r.json(t, &p)
	if r.status != 200 || p.DaysSinceLastLiquidation != 30 || p.CapitalCents != 500_000_000 || p.InterestToLiquidateCents == 0 ||
		p.TotalBalanceCents != 500_000_000+p.InterestToLiquidateCents {
		t.Fatalf("%d %s", r.status, r.body)
	}
	if bad := do(t, srv, "GET", "/api/credits/"+id+"/preview", "", ""); bad.status != 400 {
		t.Fatalf("%d %s", bad.status, bad.body)
	}

	l := do(t, srv, "POST", "/api/credits/"+id+"/interest-liquidations", uuid.NewString(), `{"date":"2026-07-01"}`)
	var m movementResponse
	l.json(t, &m)
	if l.status != 201 || m.Entry.Type != "INTERES" || m.Entry.AmountCents != p.InterestToLiquidateCents || m.Entry.Detail != "30 días al 24% EA" {
		t.Fatalf("%d %s", l.status, l.body)
	}
	nothing := do(t, srv, "POST", "/api/credits/"+id+"/interest-liquidations", uuid.NewString(), `{"date":"2026-07-01"}`)
	if nothing.status != 422 || nothing.code(t) != "NOTHING_TO_LIQUIDATE" {
		t.Fatalf("%d %s", nothing.status, nothing.body)
	}
}

func TestHealth(t *testing.T) {
	srv := newServer(t)
	if r := do(t, srv, "GET", "/api/health", "", ""); r.status != 200 {
		t.Fatalf("%d", r.status)
	}
}
