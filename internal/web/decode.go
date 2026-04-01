package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"reflect"
	"strings"
)

const (
	problemTypeUnsupportedMediaType = ProblemTypeUnsupportedMediaType
	problemTypeRequestBodyTooLarge  = ProblemTypeRequestBodyTooLarge
	problemTypeInvalidJSON          = ProblemTypeInvalidJSON
)

func DecodeJSON(w http.ResponseWriter, r *http.Request, maxBodyBytes int64, dst any) *ProblemDetail {
	if err := validateDecodeTarget(dst); err != nil {
		return &ProblemDetail{
			Type:     ProblemTypeInternalServerError,
			Title:    http.StatusText(http.StatusInternalServerError),
			Status:   http.StatusInternalServerError,
			Detail:   err.Error(),
			Instance: requestPath(r),
		}
	}

	if maxBodyBytes <= 0 {
		return &ProblemDetail{
			Type:     ProblemTypeInternalServerError,
			Title:    http.StatusText(http.StatusInternalServerError),
			Status:   http.StatusInternalServerError,
			Detail:   "invalid request body size limit",
			Instance: requestPath(r),
		}
	}

	if !isJSONContentType(r.Header.Get("Content-Type")) {
		return &ProblemDetail{
			Type:     problemTypeUnsupportedMediaType,
			Title:    http.StatusText(http.StatusUnsupportedMediaType),
			Status:   http.StatusUnsupportedMediaType,
			Detail:   "content type must be application/json",
			Instance: requestPath(r),
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)

	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()

	if err := dec.Decode(dst); err != nil {
		return decodeErrorToProblem(err, requestPath(r))
	}

	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return &ProblemDetail{
			Type:     problemTypeInvalidJSON,
			Title:    http.StatusText(http.StatusBadRequest),
			Status:   http.StatusBadRequest,
			Detail:   "request body must contain a single JSON object",
			Instance: requestPath(r),
		}
	}

	return nil
}

func isJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}

	return mediaType == "application/json"
}

func decodeErrorToProblem(err error, instance string) *ProblemDetail {
	syntaxErr, hasSyntaxErr := errors.AsType[*json.SyntaxError](err)
	unmarshalTypeErr, hasUnmarshalTypeErr := errors.AsType[*json.UnmarshalTypeError](err)
	_, hasMaxBytesErr := errors.AsType[*http.MaxBytesError](err)
	field, hasUnknownField := strings.CutPrefix(err.Error(), "json: unknown field ")
	switch {
	case hasSyntaxErr:
		return &ProblemDetail{
			Type:     problemTypeInvalidJSON,
			Title:    http.StatusText(http.StatusBadRequest),
			Status:   http.StatusBadRequest,
			Detail:   fmt.Sprintf("request body contains malformed JSON at position %d", syntaxErr.Offset),
			Instance: instance,
		}
	case errors.Is(err, io.EOF):
		return &ProblemDetail{
			Type:     problemTypeInvalidJSON,
			Title:    http.StatusText(http.StatusBadRequest),
			Status:   http.StatusBadRequest,
			Detail:   "request body must not be empty",
			Instance: instance,
		}
	case hasUnmarshalTypeErr:
		return &ProblemDetail{
			Type:     problemTypeInvalidJSON,
			Title:    http.StatusText(http.StatusBadRequest),
			Status:   http.StatusBadRequest,
			Detail:   fmt.Sprintf("request body contains an invalid value for field %q", unmarshalTypeErr.Field),
			Instance: instance,
		}
	case hasMaxBytesErr:
		return &ProblemDetail{
			Type:     problemTypeRequestBodyTooLarge,
			Title:    http.StatusText(http.StatusRequestEntityTooLarge),
			Status:   http.StatusRequestEntityTooLarge,
			Detail:   "request body is too large",
			Instance: instance,
		}
	case hasUnknownField:
		return &ProblemDetail{
			Type:     problemTypeInvalidJSON,
			Title:    http.StatusText(http.StatusBadRequest),
			Status:   http.StatusBadRequest,
			Detail:   "request body contains unknown field " + field,
			Instance: instance,
		}
	default:
		return &ProblemDetail{
			Type:     problemTypeInvalidJSON,
			Title:    http.StatusText(http.StatusBadRequest),
			Status:   http.StatusBadRequest,
			Detail:   "request body contains invalid JSON",
			Instance: instance,
		}
	}
}

func requestPath(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}

	return r.URL.Path
}

func validateDecodeTarget(dst any) error {
	v := reflect.ValueOf(dst)
	if !v.IsValid() || v.Kind() != reflect.Pointer || v.IsNil() {
		return errors.New("decode target must be a non-nil pointer")
	}

	return nil
}
