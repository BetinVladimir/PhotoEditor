package semanticreview

import (
	"context"
 "bytes"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestRemovalPresentationDocumentRechecksProducerAfterIO(t *testing.T) {
	for _, failure := range []string{"valid", "valid_buffer_reuse", "changed", "revoked", "cancel", "truncated", "corrupted", "wrong_operation", "changed_result", "changed_attempt", "changed_version", "changed_ref"} {
		p := RemovalPreviewParams{ExposurePreviewParams: previewParamsFixture(), Kind:"remove_object", ModelID:"big_lama", ContextPixels:128, FeatherPixels:8}
		run := uuid.New()
		newRef := func() v2input.Ref { return v2input.Ref{ArtifactID:uuid.New(),SHA256:p.Plan.SHA256} }
		e := RemovalPreviewPresentation{SchemaVersion:1,Params:p,NumericalEvidence:newRef(),WorkingImage:newRef(),Pixels:newRef(),
			Before:newRef(),After:newRef(),Affected:newRef(),Protected:newRef(),Width:2,Height:1,
			ICCLockSHA:p.Plan.SHA256,SRGBSHA:p.Plan.SHA256,Resize:"area_linear_premult_v1",Overlay:"max_coverage_ceil_alpha_v1"}
		raw, err := canonical.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		original := append([]byte(nil),raw...)
		sum := sha256.Sum256(raw)
		source := previewEvidenceSource{TaskID: uuid.NewSHA1(run, []byte("semantic.preview_remove_object:"+p.TargetID.String())), ResultID: uuid.New(), Attempt: 1, Fence: 1, Object: PlanSource{Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}, Bytes: int64(len(raw)), MediaType: "application/json"}}
		calls := 0
		load := func(context.Context) (previewEvidenceSource, error) {
			calls++
			if failure == "wrong_operation" {
				source.TaskID = uuid.NewSHA1(run, []byte("semantic.preview_local_exposure:"+p.TargetID.String()))
			}
			current := source
			if calls == 2 {
				if failure == "valid_buffer_reuse" { raw[0]='!' }
				if failure == "changed" {
					current.Fence++
				}
				if failure == "changed_result" { current.ResultID=uuid.New() }
				if failure == "changed_attempt" { current.Attempt++ }
				if failure == "changed_version" { current.Object.Version="replacement-version" }
				if failure == "changed_ref" { current.Object.Ref.ArtifactID=uuid.New() }
				if failure == "revoked" {
					return previewEvidenceSource{}, ErrConflict
				}
			}
			return current, nil
		}
		ctx, cancel := context.WithCancel(context.Background())
		read := func(context.Context, PlanSource) ([]byte, error) {
			if failure == "cancel" {
				cancel()
			}
			if failure == "corrupted" {
				return append([]byte("!"), raw[1:]...), nil
			}
			if failure == "truncated" {
				return raw[:len(raw)-1], nil
			}
			return raw, nil
		}
		got, err := readBoundPreviewPresentationDocument(ctx, load, read, run, uuid.NewSHA1(run, []byte("semantic.preview_remove_object:"+p.TargetID.String())))
		cancel()
		valid := failure == "valid" || failure == "valid_buffer_reuse"
		if (err == nil) != valid {
			t.Fatal(failure, err)
		}
		if valid && (calls != 2 || got.ResultID != source.ResultID || !bytes.Equal(got.raw,original)) {
			t.Fatal("missing presentation fence", got, calls)
		}
		if !valid && got.TaskID != uuid.Nil {
			t.Fatal("partial presentation escaped", failure)
		}
	}
}
