package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"swar/internal/auth"
	"swar/internal/catalog"
	"swar/internal/jam"
)

// Envelope used by every /v1 endpoint (and, with credits, by /api):
//
//	{"success": true, "data": ...}
//	{"success": false, "message": "...", "error": {"code": "..."}}
type errorBody struct {
	Code string `json:"code"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func ok(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "data": data})
}

func created(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusCreated, map[string]any{"success": true, "data": data})
}

func fail(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]any{"success": false, "message": msg, "error": errorBody{Code: code}})
}

// statusFor maps an error to an HTTP status, a stable code and a message
// that is safe to show clients (never upstream bodies or stack traces).
func statusFor(err error) (int, string, string) {
	var je *jam.Error
	if errors.As(err, &je) {
		return je.Status, je.Code, je.Message
	}
	var ce *catalog.Error
	if errors.As(err, &ce) {
		switch ce.Kind {
		case catalog.KindInvalid:
			return http.StatusBadRequest, string(ce.Kind), ce.Message
		case catalog.KindNotFound:
			return http.StatusNotFound, string(ce.Kind), ce.Message
		case catalog.KindUnavailable:
			return http.StatusForbidden, string(ce.Kind), ce.Message
		case catalog.KindRateLimited:
			return http.StatusServiceUnavailable, string(ce.Kind), ce.Message
		case catalog.KindTimeout:
			return http.StatusGatewayTimeout, string(ce.Kind), ce.Message
		case catalog.KindUpstream:
			return http.StatusBadGateway, string(ce.Kind), ce.Message
		}
	}
	if errors.Is(err, auth.ErrInvalidToken) || errors.Is(err, auth.ErrExpiredToken) {
		return http.StatusUnauthorized, "unauthorized", "Missing or invalid device token"
	}
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return http.StatusRequestEntityTooLarge, "body_too_large", "Request body is too large"
	}
	return http.StatusInternalServerError, "internal_error", "Something went wrong"
}

func (s *Server) writeErr(w http.ResponseWriter, r *http.Request, err error) {
	status, code, msg := statusFor(err)
	if status >= 500 {
		s.log.Error("request failed", "path", r.URL.Path, "status", status, "err", err, "request_id", requestID(r.Context()))
	} else {
		s.log.Debug("request rejected", "path", r.URL.Path, "status", status, "code", code)
	}
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "5")
	}
	fail(w, status, code, msg)
}

// decode reads a JSON body strictly (unknown fields rejected).
func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			return err
		}
		return catalog.Invalid("invalid JSON body: " + sanitizeJSONErr(err))
	}
	if dec.More() {
		return catalog.Invalid("invalid JSON body: trailing data")
	}
	return nil
}

func sanitizeJSONErr(err error) string {
	msg := err.Error()
	msg = strings.TrimPrefix(msg, "json: ")
	if len(msg) > 120 {
		msg = msg[:120]
	}
	return msg
}

// intParam parses an optional integer query parameter within [lo, hi].
func intParam(r *http.Request, name string, def, lo, hi int) (int, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < lo || n > hi {
		return 0, catalog.Invalid(name + " must be an integer between " + strconv.Itoa(lo) + " and " + strconv.Itoa(hi))
	}
	return n, nil
}

func logAttr(r *http.Request) slog.Attr { return slog.String("request_id", requestID(r.Context())) }
