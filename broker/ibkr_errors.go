package broker

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

type IBKRErrorClass string

const (
	IBKRErrorUnknown   IBKRErrorClass = "unknown"
	IBKRErrorTransient IBKRErrorClass = "transient"
	IBKRErrorAuth      IBKRErrorClass = "auth"
	IBKRErrorRequest   IBKRErrorClass = "request"
)

type IBKRRequestError struct {
	Operation  string
	Endpoint   string
	StatusCode int
	Class      IBKRErrorClass
	Message    string
	Cause      error
}

func (e *IBKRRequestError) Error() string {
	parts := make([]string, 0, 4)
	if e.Operation != "" {
		parts = append(parts, e.Operation)
	}
	if e.Endpoint != "" {
		parts = append(parts, e.Endpoint)
	}
	if e.StatusCode > 0 {
		parts = append(parts, fmt.Sprintf("HTTP %d", e.StatusCode))
	}
	if strings.TrimSpace(e.Message) != "" {
		parts = append(parts, strings.TrimSpace(e.Message))
	}
	if e.Cause != nil {
		parts = append(parts, e.Cause.Error())
	}
	if len(parts) == 0 {
		return "ibkr request error"
	}
	return strings.Join(parts, ": ")
}

func (e *IBKRRequestError) Unwrap() error {
	return e.Cause
}

func NewIBKRHTTPError(operation, endpoint string, statusCode int, message string) error {
	return &IBKRRequestError{
		Operation:  operation,
		Endpoint:   endpoint,
		StatusCode: statusCode,
		Class:      classifyIBKRHTTPStatus(endpoint, statusCode),
		Message:    strings.TrimSpace(message),
	}
}

func NewIBKRTransportError(operation, endpoint string, cause error) error {
	return &IBKRRequestError{
		Operation: operation,
		Endpoint:  endpoint,
		Class:     classifyIBKRTransport(cause),
		Cause:     cause,
	}
}

func ClassifyIBKRError(err error) IBKRErrorClass {
	if err == nil {
		return IBKRErrorUnknown
	}

	var reqErr *IBKRRequestError
	if errors.As(err, &reqErr) {
		if reqErr.Class != "" {
			return reqErr.Class
		}
	}

	if class := classifyIBKRTransport(err); class != IBKRErrorUnknown {
		return class
	}

	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(lower, "forbidden"),
		strings.Contains(lower, "invalid account"),
		strings.Contains(lower, "missing ibkr account id"),
		strings.Contains(lower, "authentication failed"),
		strings.Contains(lower, "credentials"),
		strings.Contains(lower, "permission denied"):
		return IBKRErrorAuth
	case strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "no connection could be made"),
		strings.Contains(lower, "timeout"),
		strings.Contains(lower, "temporarily unavailable"),
		strings.Contains(lower, "temporary failure"),
		strings.Contains(lower, "gateway unavailable"),
		strings.Contains(lower, "status 401"),
		strings.Contains(lower, "status 429"),
		strings.Contains(lower, "status 500"),
		strings.Contains(lower, "status 502"),
		strings.Contains(lower, "status 503"),
		strings.Contains(lower, "status 504"),
		strings.Contains(lower, "session not ready"),
		strings.Contains(lower, "not authenticated"),
		strings.Contains(lower, "request failed after retries"),
		strings.Contains(lower, "connection reset"),
		strings.Contains(lower, "unexpected eof"):
		return IBKRErrorTransient
	case strings.Contains(lower, "no contract found"),
		strings.Contains(lower, "invalid"),
		strings.Contains(lower, "malformed"),
		strings.Contains(lower, "rejected"),
		strings.Contains(lower, "insufficient"):
		return IBKRErrorRequest
	default:
		return IBKRErrorUnknown
	}
}

func IsRetryableIBKRError(err error) bool {
	return ClassifyIBKRError(err) == IBKRErrorTransient
}

func IsActionableIBKRError(err error) bool {
	return ClassifyIBKRError(err) == IBKRErrorAuth
}

func classifyIBKRHTTPStatus(endpoint string, statusCode int) IBKRErrorClass {
	switch statusCode {
	case 401, 408, 409, 425, 429, 500, 502, 503, 504:
		return IBKRErrorTransient
	case 403:
		return IBKRErrorAuth
	case 404:
		lowerEndpoint := strings.ToLower(strings.TrimSpace(endpoint))
		if strings.Contains(lowerEndpoint, "/portfolio/") || strings.Contains(lowerEndpoint, "/iserver/account") {
			return IBKRErrorAuth
		}
		return IBKRErrorRequest
	default:
		if statusCode >= 500 {
			return IBKRErrorTransient
		}
		if statusCode >= 400 {
			return IBKRErrorRequest
		}
	}
	return IBKRErrorUnknown
}

func classifyIBKRTransport(err error) IBKRErrorClass {
	if err == nil {
		return IBKRErrorUnknown
	}

	var netErr net.Error
	if errors.As(err, &netErr) && (netErr.Timeout() || netErr.Temporary()) {
		return IBKRErrorTransient
	}

	lower := strings.ToLower(strings.TrimSpace(err.Error()))
	if strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "no connection could be made") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "temporarily unavailable") ||
		strings.Contains(lower, "temporary failure") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "broken pipe") ||
		strings.Contains(lower, "unexpected eof") ||
		strings.Contains(lower, "eof") {
		return IBKRErrorTransient
	}

	return IBKRErrorUnknown
}
