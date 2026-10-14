package workflow

import (
	"context"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type deliveryJSONReader struct {
	body string
	fail bool
	seen objectstore.Object
}

func (r *deliveryJSONReader) Open(_ context.Context, o objectstore.Object) (io.ReadCloser, error) {
	r.seen = o
	if r.fail {
		return nil, errors.New("read failed")
	}
	return io.NopCloser(strings.NewReader(r.body)), nil
}
func TestDeliverySourceReadsOnlyExactCanonicalVersionedJSON(t *testing.T) {
	for _, scenario := range []string{"valid", "hash", "size", "oversize", "noncanonical", "read_error"} {
		t.Run(scenario, func(t *testing.T) {
			body := `{"a":1}`
			if scenario == "noncanonical" {
				body = `{ "a":1}`
			}
			reader := &deliveryJSONReader{body: body, fail: scenario == "read_error"}
			item := deliverySourceEntry{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA([]byte(body))}, Key: "tenant/pinned.json", Version: "exact-version", Bytes: int64(len(body)), MediaType: "application/json"}
			if scenario == "hash" {
				item.Ref.SHA256 = strings.Repeat("0", 64)
			}
			if scenario == "size" {
				item.Bytes++
			}
			max := int64(100)
			if scenario == "oversize" {
				max = 2
			}
			result, err := (SQLDeliveryChildResolver{Objects: reader}).readJSON(context.Background(), item, max)
			if scenario == "valid" {
				if err != nil || string(result) != body || reader.seen.VersionID != item.Version || reader.seen.SHA256 != item.Ref.SHA256 {
					t.Fatal(result, err)
				}
			} else if err == nil {
				t.Fatal("invalid physical source accepted", scenario)
			}
		})
	}
}
