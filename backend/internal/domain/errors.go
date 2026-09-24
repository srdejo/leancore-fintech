package domain

import "fmt"

// Code es un código de error estable que la API expone a los clientes.
type Code string

const (
	CodeValidation            Code = "VALIDATION"
	CodeDisbursementExceeds   Code = "DISBURSEMENT_EXCEEDS_LIMIT"
	CodeDateBeforeLastEntry   Code = "DATE_BEFORE_LAST_ENTRY"
	CodeInsufficientAvailable Code = "INSUFFICIENT_AVAILABLE"
	CodePaymentExceedsBalance Code = "PAYMENT_EXCEEDS_BALANCE"
	CodeNothingToLiquidate    Code = "NOTHING_TO_LIQUIDATE"
	CodeOverflow              Code = "AMOUNT_OUT_OF_RANGE"
)

// Error es un rechazo de una regla de negocio. Details lleva datos útiles
// para el cliente (campo inválido, disponible, saldo, fecha límite).
type Error struct {
	Code    Code
	Message string
	Details map[string]any
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func newError(code Code, msg string, details map[string]any) *Error {
	return &Error{Code: code, Message: msg, Details: details}
}

func validationError(field, msg string) *Error {
	return newError(CodeValidation, msg, map[string]any{"field": field})
}

func overflowError() *Error {
	return newError(CodeOverflow, "El monto está fuera del rango permitido.", nil)
}
