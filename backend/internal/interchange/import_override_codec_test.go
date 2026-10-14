package interchange

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

type overrideQualifierStub struct {
	calls   int
	failure error
}

func (s *overrideQualifierStub) QualifyImportedOverride(_ context.Context, raw []byte, _ v2input.SessionVersionBinding) error {
	s.calls++
	raw[0] = 'x'
	return s.failure
}

type confirmedMaskAuthorityStub struct {
	preflightCalls, remapCalls, preparedCalls int
	preflightFailure, remapFailure, failure   error
}

func (s *confirmedMaskAuthorityStub) QualifyArchivedMaskConfirmation(_ context.Context, scope ImportMaskConfirmationScope, archived review.MaskRef, old, target v2input.SessionVersionBinding) (int64, error) {
	s.preflightCalls++
	if scope.Workflow == uuid.Nil || scope.Entry == uuid.Nil || !validImportRef(scope.Recipe) || !validImportRef(scope.Override) || archived.Selector != "person" || old.AssetVersionID == target.AssetVersionID {
		return 0, v2input.ErrInvalid
	}
	if s.preflightFailure != nil {
		return 0, s.preflightFailure
	}
	return 1, nil
}

func (s *confirmedMaskAuthorityStub) QualifyImportedMaskConfirmation(_ context.Context, scope ImportMaskConfirmationScope, archived, prepared review.MaskRef, old, target v2input.SessionVersionBinding, epoch int64) error {
	s.remapCalls++
	if scope.Workflow == uuid.Nil || scope.Entry == uuid.Nil || epoch != 1 || archived.Artifact.ID == prepared.Artifact.ID || old.AssetVersionID == target.AssetVersionID {
		return v2input.ErrInvalid
	}
	return s.remapFailure
}
func (s *confirmedMaskAuthorityStub) QualifyPreparedMaskConfirmation(_ context.Context, mask review.MaskRef, target v2input.SessionVersionBinding) error {
	s.preparedCalls++
	if mask.Selector != "person" || target.AssetVersionID == uuid.Nil {
		return v2input.ErrInvalid
	}
	return s.failure
}
func TestOverrideCodecPreservesAuthoredModulesAndRemapsInlineMask(t *testing.T) {
	for _, change := range []string{"none", "candidate_review", "confirmation", "confirmation_missing_scope", "confirmation_wrong_scope", "confirmation_foreign_recipe", "confirmation_authorized", "confirmation_revoked", "confirmation_post_revoked", "source", "qualification", "cancelled"} {
		t.Run(change, func(t *testing.T) {
			_, master, imageRaw := importWorkingValidatorFixture(t)
			image, err := review.ParseWorkingImage(imageRaw)
			if err != nil {
				t.Fatal(err)
			}
			canvas, err := canonical.Hash(image.Canvas)
			if err != nil {
				t.Fatal(err)
			}
			old := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: v2input.Ref{ArtifactID: uuid.New(), SHA256: master.Ref.SHA256}, CanvasHash: canvas}
			target := old
			target.AssetVersionID = uuid.New()
			target.Source = master.Ref
			pixels := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("mask"))}
			mask := map[string]any{"artifact": pixels, "canvas": image.Canvas, "transform_chain": json.RawMessage(image.TransformChain), "object_id": nil, "selector": "manual", "confidence": 1, "model_lock_id": nil, "protected": true}
			binding := map[string]any{"binding_id": "guard-1", "purpose": "protected", "mask": mask, "strength": .6}
			if change == "confirmation" || change == "confirmation_missing_scope" || change == "confirmation_wrong_scope" || change == "confirmation_foreign_recipe" || change == "confirmation_authorized" || change == "confirmation_revoked" || change == "confirmation_post_revoked" {
				binding["confirmed"] = true
			}
			if change == "confirmation_missing_scope" || change == "confirmation_wrong_scope" || change == "confirmation_foreign_recipe" || change == "confirmation_authorized" || change == "confirmation_revoked" || change == "confirmation_post_revoked" {
				mask["selector"] = "person"
				mask["model_lock_id"] = rawSHA([]byte("model"))
			}
			modules := map[string]any{"strength": .7, "beauty": map[string]any{"enabled": false}}
			doc := map[string]any{"schema_version": 1, "input_sha256": image.Canvas.SourceSHA256, "object_id": "portable-region", "modules": modules, "mask_refs": []any{binding}, "protected_polygons": []any{}, "manual_landmarks": nil, "revision": 7}
			if change == "candidate_review" {
				doc["accepted_candidate_ids"] = []string{"foreign-candidate"}
			}
			if change == "source" {
				doc["input_sha256"] = rawSHA([]byte("foreign source"))
			}
			raw, err := canonical.Marshal(doc)
			if err != nil {
				t.Fatal(err)
			}
			qualifier := &overrideQualifierStub{}
			denied := errors.New("module qualification failed")
			if change == "qualification" {
				qualifier.failure = denied
			}
			masters := &importMasterReaderStub{body: imageRaw}
			maskQualifier := &importMaskQualifierStub{}
			codec := ImportOverrideCodec{Masters: masters, Masks: ImportMaskCodec{Qualifier: maskQualifier}, Qualifier: qualifier}
			authority := &confirmedMaskAuthorityStub{}
			if change == "confirmation_missing_scope" || change == "confirmation_wrong_scope" || change == "confirmation_foreign_recipe" || change == "confirmation_authorized" || change == "confirmation_revoked" || change == "confirmation_post_revoked" {
				codec.Confirmed = authority
			}
			if change == "confirmation_revoked" {
				authority.preflightFailure = denied
			}
			if change == "confirmation_post_revoked" {
				authority.remapFailure = denied
			}
			resolveCalls := 0
			resolve := func(ref v2input.Ref, kind string) (v2input.Ref, error) {
				resolveCalls++
				if kind != "MaskPixels" && kind != "DenseWarpField" {
					return v2input.Ref{}, v2input.ErrInvalid
				}
				return v2input.Ref{ArtifactID: uuid.NewSHA1(target.AssetVersionID, []byte(ref.ArtifactID.String())), SHA256: ref.SHA256}, nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if change == "confirmation_authorized" || change == "confirmation_revoked" || change == "confirmation_post_revoked" {
				workflow, entry := uuid.New(), uuid.New()
				override := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}
				recipe := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("recipe"))}
				ctx = withImportRecipeConfirmationScope(ctx, workflow, entry, recipe, []v2input.Ref{override})
				ctx = withImportConfirmationScope(ctx, workflow, entry, override, "Override")
			}
			if change == "confirmation_wrong_scope" {
				workflow, entry := uuid.New(), uuid.New()
				override := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("other override"))}
				recipe := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("recipe"))}
				ctx = withImportRecipeConfirmationScope(ctx, workflow, entry, recipe, []v2input.Ref{override})
				ctx = withImportConfirmationScope(ctx, workflow, entry, override, "Override")
			}
			if change == "confirmation_foreign_recipe" {
				workflow, entry := uuid.New(), uuid.New()
				override := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA(raw)}
				recipe := v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte("recipe"))}
				foreign := v2input.Ref{ArtifactID: uuid.New(), SHA256: override.SHA256}
				ctx = withImportRecipeConfirmationScope(ctx, workflow, entry, recipe, []v2input.Ref{foreign})
				ctx = withImportConfirmationScope(ctx, workflow, entry, override, "Override")
			}
			if change == "cancelled" {
				cancel()
			}
			prepared, err := codec.RemapImportDocument(ctx, "Override", raw, old, target, resolve)
			if change != "none" && change != "confirmation_authorized" {
				if err == nil {
					t.Fatal("unqualified override accepted")
				}
				if (change == "qualification" || change == "confirmation_revoked" || change == "confirmation_post_revoked") && !errors.Is(err, denied) {
					t.Fatal("qualification error lost", err)
				}
				if change == "cancelled" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
				if change == "candidate_review" || change == "confirmation" {
					var unsupported *ImportRecipeUnsupported
					if !errors.As(err, &unsupported) {
						t.Fatal("review authority copied", err)
					}
				}
				if change == "confirmation_revoked" && (authority.preflightCalls != 1 || authority.remapCalls != 0 || resolveCalls != 0 || maskQualifier.calls != 0) {
					t.Fatal("revoked confirmation reached mask remap")
				}
				if (change == "confirmation_missing_scope" || change == "confirmation_wrong_scope" || change == "confirmation_foreign_recipe") && (authority.preflightCalls != 0 || resolveCalls != 0) {
					t.Fatal("unscoped confirmation reached authority or remap")
				}
				if change == "confirmation_post_revoked" && (authority.preflightCalls != 1 || authority.remapCalls != 1) {
					t.Fatal("new asset confirmation not rechecked")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if change == "confirmation_authorized" && (authority.preflightCalls != 1 || authority.remapCalls != 1) {
				t.Fatal("new asset confirmation not checked")
			}
			var result map[string]json.RawMessage
			if err := json.Unmarshal(prepared, &result); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"modules", "revision", "protected_polygons", "manual_landmarks", "input_sha256"} {
				expected, err := canonical.Marshal(doc[key])
				if err != nil || string(expected) != string(result[key]) {
					t.Fatal("authored field changed", key, err)
				}
			}
			if string(result["object_id"]) == `"portable-region"` || qualifier.calls != 1 || masters.calls != 2 {
				t.Fatal("object identity or source recheck missing")
			}
			var masks []struct {
				Mask review.MaskRef `json:"mask"`
			}
			if err := json.Unmarshal(result["mask_refs"], &masks); err != nil || len(masks) != 1 || masks[0].Mask.Artifact.ID == pixels.ArtifactID || !masks[0].Mask.Protected {
				t.Fatal("inline mask not remapped", err)
			}
		})
	}
}
