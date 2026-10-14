package workflow

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestDeliverySourceProjectionExcludesUnrelatedAndPinsNestedRefs(t *testing.T) {
	pixel := v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA([]byte("binary"))}
	unrelated := v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA([]byte("other"))}
	body, err := canonical.Marshal(map[string]any{"pixels": pixel})
	if err != nil {
		t.Fatal(err)
	}
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: renderDerivativeBytesSHA(body)}
	metadata := map[uuid.UUID]deliverySourceEntry{
		root.ArtifactID:      {Ref: root, Contract: "WorkingImage", MediaType: "application/json", Bytes: int64(len(body))},
		pixel.ArtifactID:     {Ref: pixel, Contract: "NpyArray", MediaType: "application/octet-stream", Bytes: 6},
		unrelated.ArtifactID: {Ref: unrelated, Contract: "NpyArray", MediaType: "application/octet-stream", Bytes: 5},
	}
	readCount := 0
	read := func(_ context.Context, item deliverySourceEntry) ([]byte, error) {
		readCount++
		if item.Ref != root {
			t.Fatal("binary read")
		}
		return body, nil
	}
	entries, err := projectDeliverySources(context.Background(), []v2input.Ref{root, root}, metadata, read)
	if err != nil || len(entries) != 2 || readCount != 1 {
		t.Fatal(entries, readCount, err)
	}
	for _, entry := range entries {
		if entry.Ref == unrelated {
			t.Fatal("unrelated retained")
		}
	}
	delete(metadata, pixel.ArtifactID)
	if _, err := projectDeliverySources(context.Background(), []v2input.Ref{root}, metadata, read); err == nil {
		t.Fatal("missing dependency accepted")
	}
	changed := pixel
	changed.SHA256 = unrelated.SHA256
	metadata[pixel.ArtifactID] = deliverySourceEntry{Ref: changed, Contract: "NpyArray", MediaType: "application/octet-stream", Bytes: 6}
	if _, err := projectDeliverySources(context.Background(), []v2input.Ref{root}, metadata, read); err == nil {
		t.Fatal("substituted dependency accepted")
	}
}
func TestDeliveryJSONRefsAcceptsBothWireFormsAndRefusesMalformedRefs(t *testing.T) {
	id := uuid.New()
	sha := renderDerivativeBytesSHA([]byte("a"))
	body, _ := json.Marshal(map[string]any{"a": map[string]any{"id": id, "sha256": sha}, "b": []any{map[string]any{"artifact_id": id, "sha256": sha}}})
	refs, err := deliveryJSONRefs(body)
	if err != nil || len(refs) != 2 || refs[0] != refs[1] {
		t.Fatal(refs, err)
	}
	for _, raw := range []string{`{"artifact_id":"bad"}`, `{"artifact_id":"` + id.String() + `","sha256":"bad"}`, `{"id":"bad","sha256":"` + sha + `"}`} {
		if _, err := deliveryJSONRefs([]byte(raw)); err == nil {
			t.Fatal("malformed ref accepted", raw)
		}
	}
}
