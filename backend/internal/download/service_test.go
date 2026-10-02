package download

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestIntentIdempotencyRecordContainsNoBearer(t *testing.T) {
	raw, err := json.Marshal(intentRecord{ID: uuid.New(), ExpiresAt: time.Unix(1, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("token")) {
		t.Fatalf("idempotency record leaks bearer field: %s", raw)
	}
}
