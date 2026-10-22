package interactive

import (
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
)

// Accepted receipts are immutable admission tickets, not current run status.
// Recovery must not turn a malformed successful ledger row into a new task.
func validateReceipt(out idempotency.Response, sourceSHA, key string, allowAbandoned bool) error {
	invalid := func() error { return domain.NewError("IDEMPOTENCY_CONFLICT", 409) }
	var fields map[string]json.RawMessage
	if json.Unmarshal(out.Body, &fields) != nil || fields == nil {
		return invalid()
	}
	var status string
	if json.Unmarshal(fields["status"], &status) != nil {
		return invalid()
	}
	if out.Status == 200 && allowAbandoned {
		var storedKey string
		if len(fields) != 2 || status != "abandoned" || out.ResourceID != nil ||
			json.Unmarshal(fields["key"], &storedKey) != nil || storedKey != key || !idempotency.ValidKey(key) {
			return invalid()
		}
		return nil
	}
	var run uuid.UUID
	if out.Status != 202 || status != "queued" || json.Unmarshal(fields["run_id"], &run) != nil ||
		run == uuid.Nil || out.ResourceID == nil || *out.ResourceID != run {
		return invalid()
	}
	if sourceSHA != "" {
		var storedSHA string
		if len(fields) != 3 || json.Unmarshal(fields["source_sha256"], &storedSHA) != nil || storedSHA != sourceSHA {
			return invalid()
		}
	} else if len(fields) != 2 {
		return invalid()
	}
	return nil
}
