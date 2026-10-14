package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type importDefectReviewAuthorityStub struct{ imported, prepared int }

func (s *importDefectReviewAuthorityStub) ReissueImportedDefectReview(_ context.Context, archived, current review.DefectSelection, old, target v2input.SessionVersionBinding) (review.DefectSelection, error) {
	s.imported++
	if archived.Image.ID != old.Source.ArtifactID || current.Image.ID != target.Source.ArtifactID || archived.Proposal == current.Proposal {
		return review.DefectSelection{}, v2input.ErrInvalid
	}
	current.Accepted = []string{rawSHA([]byte("target candidate"))}
	return current, nil
}
func (s *importDefectReviewAuthorityStub) QualifyPreparedDefectReview(_ context.Context, current review.DefectSelection, target v2input.SessionVersionBinding) error {
	s.prepared++
	if current.Image.ID != target.Source.ArtifactID {
		return v2input.ErrInvalid
	}
	return nil
}

func TestImportDefectReviewRequiresFreshAuthority(t *testing.T) {
	ref := func(label string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte(label))}
	}
	old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref("old"), CanvasHash: rawSHA([]byte("canvas"))}
	target := old
	target.AssetVersionID = uuid.New()
	target.Source = ref("target")
	raw, err := canonical.Marshal(map[string]any{"version": "defect_review_v1", "proposal": ref("proposal"), "image": old.Source, "label_map": ref("labels"), "revision": 1, "accepted_candidate_ids": []string{rawSHA([]byte("candidate"))}, "rejected_candidate_ids": []string{}})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	resolve := func(source v2input.Ref, kind string) (v2input.Ref, error) {
		calls++
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: source.SHA256}, nil
	}
	if _, err := (ImportDefectReviewCodec{}).RemapImportDocument(context.Background(), "DefectReview", raw, old, target, resolve); err == nil || calls != 0 {
		t.Fatal("archived decisions accepted without fresh authority", err)
	}
	authority := &importDefectReviewAuthorityStub{}
	prepared, err := (ImportDefectReviewCodec{Authority: authority}).RemapImportDocument(context.Background(), "DefectReview", raw, old, target, resolve)
	if err != nil || calls != 2 || authority.imported != 1 || authority.prepared != 1 || len(prepared) == 0 {
		t.Fatal("typed review remap failed", err, calls, authority)
	}
	selection, err := review.ParseDefectSelection(prepared, review.ArtifactRef{ID: uuid.New(), SHA256: rawSHA(prepared)})
	if err != nil || len(selection.Accepted) != 1 || selection.Accepted[0] != rawSHA([]byte("target candidate")) {
		t.Fatal("target candidate decision was not reissued", err)
	}
}
