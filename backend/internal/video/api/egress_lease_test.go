package videoapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestEgressLeaseBytesVectors(t *testing.T) {
	raw, err := os.ReadFile("../../../../docs/video/vectors/scale.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Cases []struct {
			Remaining int64  `json:"remaining"`
			Streams   int    `json:"streams"`
			Expect    int64  `json:"expect"`
			Error     string `json:"error"`
		} `json:"egress_lease"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Cases) != 12 {
		t.Fatalf("vector has %d cases, want 12", len(doc.Cases))
	}
	for i, c := range doc.Cases {
		got, err := EgressLeaseBytes(c.Remaining, c.Streams)
		if c.Error != "" {
			if err == nil || !strings.Contains(err.Error(), c.Error) {
				t.Errorf("case %d: want %s, got %v", i, c.Error, err)
			}
			continue
		}
		if err != nil || got != c.Expect {
			t.Errorf("case %d (%d,%d): got %d,%v want %d", i, c.Remaining, c.Streams, got, err, c.Expect)
		}
	}
}
