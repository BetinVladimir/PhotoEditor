package gateway

import "log/slog"

// One warning per request, containing only closed vocabulary and scalar counts.
// Observation must never change stream authority, cancellation or failure behavior.
type downloadStreamObservation struct{ emitted bool }

func (o *downloadStreamObservation) emit(stage, boundary string, transferred, expected int64) {
	if o.emitted || transferred < 0 || expected < 0 || transferred > expected {
		return
	}
	switch stage {
	case "context_done", "authority_identity_changed", "authority_context_done", "authority_query_failed", "authority_expired", "read_overrun", "write_failed", "write_timeout", "short_write", "short_eof", "read_failed":
	default:
		return
	}
	switch boundary {
	case "initial", "before_read", "after_read", "write", "eof":
	default:
		return
	}
	o.emitted = true
	// A broken diagnostic handler must not replace the original stream outcome.
	defer func() { _ = recover() }()
	slog.Warn("gateway stream interrupted", "stage", stage, "boundary", boundary, "bytes_transferred", transferred, "expected_bytes", expected)
}
