// Package models holds shared types + sentinel errors used across
// handlers and the repository.
package models

import "errors"

// ErrNotFound is returned by repository lookups when the requested
// resource does not exist. Handlers translate this to HTTP 404 with
// the Genesys-style error body.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when an operation would violate a uniqueness
// or FK constraint, or when a parent resource has dependent children
// and is being deleted. Handlers translate this to HTTP 409.
var ErrConflict = errors.New("conflict")

// ErrBadRequest is returned when an inbound request fails server-side
// validation (e.g. missing required field). Handlers translate to 400.
var ErrBadRequest = errors.New("bad request")

// PagedResponse mirrors the Genesys Cloud paginated list envelope.
// Every list endpoint that supports pagination returns this shape.
//
//	{
//	  "entities":   [...],
//	  "pageCount":  3,
//	  "pageNumber": 1,
//	  "pageSize":   25,
//	  "total":      57
//	}
type PagedResponse[T any] struct {
	Entities   []T `json:"entities"`
	PageCount  int `json:"pageCount"`
	PageNumber int `json:"pageNumber"`
	PageSize   int `json:"pageSize"`
	Total      int `json:"total"`
}

// ErrorResponse is the JSON body fakegenesys returns for non-2xx
// responses. Mirrors the Genesys Cloud public error shape:
//
//	{
//	  "status":          404,
//	  "code":            "not.found",
//	  "message":         "human-readable",
//	  "messageWithParams": "...",
//	  "contextId":       "<uuid>"
//	}
//
// fakegenesys populates status + code + message; contextId is a fresh
// UUID so multi-request flows can be correlated in tests.
type ErrorResponse struct {
	Status            int    `json:"status"`
	Code              string `json:"code"`
	Message           string `json:"message"`
	MessageWithParams string `json:"messageWithParams,omitempty"`
	ContextID         string `json:"contextId,omitempty"`
}
