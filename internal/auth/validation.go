package auth

import "net/http"

type validationOutcome string

const (
	validationOutcomeAuthorized   validationOutcome = "authorized"
	validationOutcomeUnauthorized validationOutcome = "unauthorized"
	validationOutcomeExpired      validationOutcome = "expired"
	validationOutcomeRevoked      validationOutcome = "revoked"
	validationOutcomeExhausted    validationOutcome = "exhausted"
	validationOutcomeError        validationOutcome = "error"
)

func (o validationOutcome) httpStatus() int {
	switch o {
	case validationOutcomeUnauthorized:
		return http.StatusUnauthorized
	case validationOutcomeExpired, validationOutcomeRevoked:
		return http.StatusForbidden
	case validationOutcomeExhausted:
		return http.StatusTooManyRequests
	case validationOutcomeError:
		return http.StatusServiceUnavailable
	default:
		return http.StatusOK
	}
}

func (o validationOutcome) errorMessage() string {
	switch o {
	case validationOutcomeUnauthorized:
		return "unauthorized"
	case validationOutcomeExpired, validationOutcomeRevoked:
		return "key inactive"
	case validationOutcomeExhausted:
		return "usage limit reached"
	case validationOutcomeError:
		return "service unavailable"
	default:
		return ""
	}
}
