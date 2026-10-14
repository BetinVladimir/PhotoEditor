package v2portrait

import (
	"context"
	"encoding/json"
	"photoeditor/backend/internal/canonical"
	"testing"
)

func TestAnalysisRestorePublishedTransportRejectsMalformedAndChangedMetadata(t *testing.T) {
	request := analysisPlanFixture(t)
	plan, err := CompileRelightAnalysisPlan(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	graph, err := plan.PublishInputClosure(context.Background(), &analysisTransportPublisher{})
	if err != nil {
		t.Fatal(err)
	}
	wire := map[string]any{"manifest": graph.Built.Manifest, "chunks": graph.Built.Chunks, "count": graph.Built.Count, "sha256": graph.Built.SHA256}
	raw, err := canonical.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := plan.RestorePublishedTransport(context.Background(), raw)
	if err != nil || restored.ID != graph.ID {
		t.Fatal(err)
	}
	for _, fault := range []string{"extra", "count", "sha", "null_chunks"} {
		var changed map[string]any
		if json.Unmarshal(raw, &changed) != nil {
			t.Fatal("fixture")
		}
		switch fault {
		case "extra":
			changed["unknown"] = true
		case "count":
			changed["count"] = 999
		case "sha":
			changed["sha256"] = "invalid"
		case "null_chunks":
			changed["chunks"] = nil
		}
		body, err := canonical.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := plan.RestorePublishedTransport(context.Background(), body); err == nil {
			t.Fatal("accepted", fault)
		}
	}
	for _, body := range [][]byte{nil, []byte("null"), []byte("{}"), []byte("{broken}")} {
		if _, err := plan.RestorePublishedTransport(context.Background(), body); err == nil {
			t.Fatal("accepted malformed metadata")
		}
	}
}
