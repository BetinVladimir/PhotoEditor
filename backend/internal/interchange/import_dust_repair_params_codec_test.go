package interchange

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type dustParamsFallbackStub struct{ calls int }

func (s *dustParamsFallbackStub) RemapImportDocument(context.Context, string, []byte, v2input.SessionVersionBinding, v2input.SessionVersionBinding, func(v2input.Ref, string) (v2input.Ref, error)) ([]byte, error) {
	s.calls++
	return nil, nil
}
func (s *dustParamsFallbackStub) QualifyRemappedImportDocument(context.Context, string, []byte, v2input.SessionVersionBinding) error {
	s.calls++
	return nil
}

type dustRepairOperationStub struct{ calls int }

func (s *dustRepairOperationStub) QualifyImportedLayerOperation(_ context.Context, operation, version string, _ []byte, _ v2input.SessionVersionBinding) error {
	s.calls++
	if operation != "sensor.dust_repair" || version != "1" {
		return v2input.ErrInvalid
	}
	return nil
}

type dustReviewIssuerStub struct{}

func (dustReviewIssuerStub) ReissueImportedDustReview(_ context.Context, _, current review.DustSelection, _, _ v2input.SessionVersionBinding) (review.DustSelection, error) {
	current.Accepted = []string{rawSHA([]byte("new candidate"))}
	current.Rejected = []string{}
	return current, nil
}
func (dustReviewIssuerStub) QualifyPreparedDustReview(context.Context, review.DustSelection, v2input.SessionVersionBinding) error {
	return nil
}

func TestImportDustRepairParamsRequiresNewReviewedDecision(t *testing.T) {
	ref := func(s string) v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte(s))} }
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref("old image"), CanvasHash: rawSHA([]byte("canvas"))}
	target := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref("new image"), CanvasHash: old.CanvasHash}
	proposal := ref("old proposal")
	reviewed := review.DustSelection{Version: "sensor_dust_review_v1", Proposal: review.ArtifactRef{ID: proposal.ArtifactID, SHA256: proposal.SHA256}, Base: review.ArtifactRef{ID: old.Source.ArtifactID, SHA256: old.Source.SHA256}, Revision: 2, Accepted: []string{rawSHA([]byte("old candidate"))}, Rejected: []string{}}
	raw, err := canonical.Marshal(importDustRepairParams{Image: old.Source, Proposal: &proposal, Review: &reviewed})
	if err != nil {
		t.Fatal(err)
	}
	resolve := func(source v2input.Ref, kind string) (v2input.Ref, error) {
		if source != proposal || kind != "DustCandidates" {
			return v2input.Ref{}, v2input.ErrInvalid
		}
		return ref("new proposal"), nil
	}
	operation := &dustRepairOperationStub{}
	if _, err := (ImportDustRepairParamsCodec{Qualifier: operation}).RemapImportDocument(context.Background(), "Params", raw, old, target, resolve); err == nil || operation.calls != 0 {
		t.Fatal("archived dust decision accepted without fresh issuer", err)
	}
	var malformed map[string]json.RawMessage
	if err := json.Unmarshal(raw, &malformed); err != nil {
		t.Fatal(err)
	}
	malformed["extra"] = json.RawMessage(`true`)
	withExtra, err := canonical.Marshal(malformed)
	if err != nil {
		t.Fatal(err)
	}
	fallback := &dustParamsFallbackStub{}
	if _, err := (ImportDustRepairParamsCodec{Qualifier: operation, Authority: dustReviewIssuerStub{}, Fallback: fallback}).RemapImportDocument(context.Background(), "Params", withExtra, old, target, resolve); err == nil || fallback.calls != 0 {
		t.Fatal("malformed dust Params reached generic fallback", err)
	}
	prepared, err := (ImportDustRepairParamsCodec{Qualifier: operation, Authority: dustReviewIssuerStub{}}).RemapImportDocument(context.Background(), "Params", raw, old, target, resolve)
	if err != nil || operation.calls != 1 {
		t.Fatal("reviewed dust Params remap failed", err)
	}
	p, err := decodeImportDustRepairParams(prepared)
	if err != nil || p.Image != target.Source || p.Review.Accepted[0] == reviewed.Accepted[0] {
		t.Fatal("archived dust selection was retained", err)
	}
}
