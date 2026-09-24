package httpapi

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"leancore-fintech/backend/internal/application/ports"
	"leancore-fintech/backend/internal/domain"
)

// --- Requests --------------------------------------------------------------
// Los montos se decodifican como json.Number y se validan como enteros: nunca
// pasan por float64.

type createCreditRequest struct {
	Holder       string      `json:"holder"`
	LimitCents   json.Number `json:"limitCents"`
	RateEABps    json.Number `json:"rateEaBps"`
	Disbursement struct {
		AmountCents json.Number `json:"amountCents"`
		Date        string      `json:"date"`
		Description string      `json:"description"`
	} `json:"disbursement"`
}

type movementRequest struct {
	AmountCents json.Number `json:"amountCents"`
	Date        string      `json:"date"`
	Description string      `json:"description"`
}

type liquidateRequest struct {
	Date string `json:"date"`
}

// parseInteger acepta solo literales enteros (sin punto ni exponente).
func parseInteger(n json.Number, field string) (int64, error) {
	s := strings.TrimSpace(string(n))
	if s == "" {
		return 0, nil // ausente: lo valida el dominio
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, &apiError{status: 400, Code: "INVALID_AMOUNT",
			Message: "El valor debe ser un entero (montos en centavos).", Details: map[string]any{"field": field}}
	}
	return v, nil
}

func parseDate(s, field string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil // ausente: lo valida el dominio
	}
	t, err := time.Parse(domain.DateLayout, s)
	if err != nil {
		return time.Time{}, &apiError{status: 400, Code: "INVALID_DATE",
			Message: "La fecha debe tener el formato AAAA-MM-DD.", Details: map[string]any{"field": field}}
	}
	return t, nil
}

// --- Responses -------------------------------------------------------------

type creditSummary struct {
	ID               string `json:"id"`
	AccountNumber    string `json:"accountNumber"`
	Holder           string `json:"holder"`
	LimitCents       int64  `json:"limitCents"`
	RateEABps        int    `json:"rateEaBps"`
	CapitalCents     int64  `json:"capitalCents"`
	InterestDueCents int64  `json:"interestDueCents"`
	BalanceCents     int64  `json:"balanceCents"`
	AvailableCents   int64  `json:"availableCents"`
	UsedBps          int64  `json:"usedBps"`
	Status           string `json:"status"`
	LastEntryDate    string `json:"lastEntryDate"`
}

type creditDetail struct {
	creditSummary
	ProjectionDate        string `json:"projectionDate"`
	AccruedInterestCents  int64  `json:"accruedInterestCents"`
	ProjectedBalanceCents int64  `json:"projectedBalanceCents"`
}

type allocationResponse struct {
	UseEntryID     string `json:"useEntryId"`
	UseSeq         int    `json:"useSeq"`
	ToCapitalCents int64  `json:"toCapitalCents"`
}

type entryResponse struct {
	ID                string               `json:"id"`
	Seq               int                  `json:"seq"`
	Type              string               `json:"type"`
	AmountCents       int64                `json:"amountCents"`
	Date              string               `json:"date"`
	Description       string               `json:"description"`
	Detail            string               `json:"detail"`
	ToInterestCents   *int64               `json:"toInterestCents,omitempty"`
	ToCapitalCents    *int64               `json:"toCapitalCents,omitempty"`
	Allocations       []allocationResponse `json:"allocations,omitempty"`
	BalanceAfterCents int64                `json:"balanceAfterCents"`
}

type entriesResponse struct {
	Entries          []entryResponse `json:"entries"`
	TotalDebitCents  int64           `json:"totalDebitCents"`
	TotalCreditCents int64           `json:"totalCreditCents"`
}

type previewResponse struct {
	Date                     string `json:"date"`
	CapitalCents             int64  `json:"capitalCents"`
	InterestDueCents         int64  `json:"interestDueCents"`
	DaysSinceLastLiquidation int64  `json:"daysSinceLastLiquidation"`
	InterestToLiquidateCents int64  `json:"interestToLiquidateCents"`
	TotalBalanceCents        int64  `json:"totalBalanceCents"`
}

// createCreditResponse solo lleva datos inmutables, para que un reintento
// idempotente devuelva exactamente el mismo cuerpo.
type createCreditResponse struct {
	ID            string        `json:"id"`
	AccountNumber string        `json:"accountNumber"`
	Holder        string        `json:"holder"`
	LimitCents    int64         `json:"limitCents"`
	RateEABps     int           `json:"rateEaBps"`
	Disbursement  entryResponse `json:"disbursement"`
}

type movementResponse struct {
	Entry entryResponse `json:"entry"`
}

func toSummary(cl *domain.CreditLine) creditSummary {
	return creditSummary{
		ID: cl.ID, AccountNumber: cl.AccountNumber, Holder: cl.Holder, LimitCents: int64(cl.Limit),
		RateEABps: cl.RateEABps, CapitalCents: int64(cl.Capital), InterestDueCents: int64(cl.InterestDue),
		BalanceCents: int64(cl.Balance()), AvailableCents: int64(cl.Available()), UsedBps: cl.UsedBps(),
		Status: string(cl.Status()), LastEntryDate: cl.LastEntryDate.Format(domain.DateLayout),
	}
}

func toDetail(v ports.CreditView) creditDetail {
	return creditDetail{
		creditSummary:         toSummary(v.Credit),
		ProjectionDate:        v.Projection.Date.Format(domain.DateLayout),
		AccruedInterestCents:  int64(v.Projection.InterestToLiquidate),
		ProjectedBalanceCents: int64(v.Projection.TotalBalance),
	}
}

func toEntry(e domain.Entry) entryResponse {
	r := entryResponse{
		ID: e.ID, Seq: e.Seq, Type: string(e.Type), AmountCents: int64(e.Amount),
		Date: e.Date.Format(domain.DateLayout), Description: e.Description, Detail: e.Detail,
		BalanceAfterCents: int64(e.BalanceAfter),
	}
	if e.Type == domain.EntryPayment {
		toInt := int64(e.ToInterest)
		toCap := int64(e.Amount - e.ToInterest)
		r.ToInterestCents, r.ToCapitalCents = &toInt, &toCap
		for _, a := range e.Allocations {
			r.Allocations = append(r.Allocations, allocationResponse{a.UseEntryID, a.UseSeq, int64(a.ToCapital)})
		}
	}
	return r
}

func toPreview(p domain.Preview) previewResponse {
	return previewResponse{
		Date: p.Date.Format(domain.DateLayout), CapitalCents: int64(p.Capital), InterestDueCents: int64(p.InterestDue),
		DaysSinceLastLiquidation: p.DaysSinceAnchor, InterestToLiquidateCents: int64(p.InterestToLiquidate),
		TotalBalanceCents: int64(p.TotalBalance),
	}
}
