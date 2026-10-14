package v2optical

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
)

func TestMultiIntentPreservesFrameOrderAndRejectsForeignFields(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			_, _, _, _, params, _ := multiQualifiedFixture(t, operation)
			intent := MultiIntent{SchemaVersion: 1, Snapshot: uuid.New(), Params: params}
			body, err := canonical.Marshal(intent)
			if err != nil {
				t.Fatal(err)
			}
			decoded, err := DecodeMultiIntent(operation, body)
			if err != nil || decoded.Snapshot != intent.Snapshot || !bytes.Equal(decoded.Params, params) {
				t.Fatal("lost ordered Params", err)
			}
			body[0] = ' '
			if !bytes.Equal(decoded.Params, params) {
				t.Fatal("intent aliases request")
			}
			for _, fault := range []string{"outer", "source_bytes", "params", "schema", "snapshot"} {
				raw, err := canonical.Marshal(intent)
				if err != nil {
					t.Fatal(err)
				}
				var value map[string]any
				if json.Unmarshal(raw, &value) != nil {
					t.Fatal("fixture")
				}
				switch fault {
				case "outer":
					value["operation"] = operation
				case "source_bytes":
					value["sources"] = []any{}
				case "params":
					value["params"].(map[string]any)["runtime_lock_sha"] = "foreign"
				case "schema":
					value["schema_version"] = 2
				case "snapshot":
					value["snapshot_id"] = uuid.Nil.String()
				}
				raw, err = canonical.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := DecodeMultiIntent(operation, raw); err == nil {
					t.Fatal("accepted foreign intent", fault)
				}
			}
		})
	}
}
