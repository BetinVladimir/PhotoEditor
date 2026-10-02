package v2learning

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

func TestBuildTrainingCaseEvidenceDerivesTargetFromVerifiedRecipes(t *testing.T) {
	run, event, session, asset, approval := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	ref := func(letter string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat(letter, 64)}
	}
	working, baseDoc, approvedDoc := ref("1"), ref("2"), ref("3")
	binding := v2input.SessionVersionBinding{AssetVersionID: asset,
		Source: working, CanvasHash: strings.Repeat("4", 64)}
	icc, model := strings.Repeat("5", 64), strings.Repeat("6", 64)
	base := FrozenRecipe{SchemaVersion: 1, Source: binding, BaseStyle: ref("7"),
		Renderer: "v2.2", ICCLockSHA: icc, ModelLockSHA: model}
	approved := base
	approved.EffectiveDelta = v2draft.CorrectionVector{EV: 0.25}
	body := func(recipe FrozenRecipe) ([]byte, string) {
		raw, err := canonical.Marshal(recipe)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		return raw, hex.EncodeToString(sum[:])
	}
	baseBody, baseSHA := body(base)
	approvedBody, approvedSHA := body(approved)
	compiled := CompiledTrainingRecipes{Base: base, Approved: approved,
		BaseBody: baseBody, BaseSHA: baseSHA,
		ApprovedBody: approvedBody, ApprovedSHA: approvedSHA}
	candidate := TrainingCandidate{EventID: event, SessionID: session,
		AssetVersionID: asset, SourceContentSHA: strings.Repeat("8", 64),
		SourceLineageSHA: strings.Repeat("9", 64),
		BaseDocumentID:   uuid.New(), BaseArtifactID: baseDoc.ArtifactID,
		BaseSHA: baseDoc.SHA256, ApprovedDocumentID: uuid.New(),
		ApprovedArtifactID: approvedDoc.ArtifactID,
		ApprovedSHA:        approvedDoc.SHA256, ApprovalID: &approval,
		ApprovedAt: time.Now().UTC(), ConsentID: uuid.New(), ConsentEpoch: 1,
		ChangedPaths: []string{"/ev"}, ConsentLive: true,
		SourceReady: true, BaseReady: true, ApprovedReady: true}
	render := TrainingRenderCase{Binding: binding, WorkingImage: working,
		Protected: ref("a"), Faces: ref("b"), DescriptorShard: ref("c"),
		DescriptorAnalysisID: uuid.New(), DescriptorOrdinal: 0}
	evidence, err := BuildTrainingCaseEvidence(run, candidate, [12]float64{},
		render, compiled, icc, model)
	if err != nil || evidence.Source.Target[0] != .25 ||
		evidence.Resolved.Render.BaseDocument != baseDoc ||
		evidence.Resolved.Render.ApprovedDocument != approvedDoc {
		t.Fatalf("incorrect derived evidence: %v", err)
	}
	descriptor := TrainingDescriptor{Features: [12]float64{}, Faces: render.Faces,
		Shard: render.DescriptorShard, AnalysisID: render.DescriptorAnalysisID,
		Ordinal: render.DescriptorOrdinal}
	fromDescriptor, err := BuildTrainingCaseFromDescriptor(run, candidate,
		descriptor, render, compiled, icc, model)
	if err != nil || fromDescriptor.Resolved.Render.DescriptorShard != descriptor.Shard ||
		fromDescriptor.Resolved.Render.DescriptorAnalysisID != descriptor.AnalysisID {
		t.Fatalf("descriptor provenance missing: %v", err)
	}
	descriptor.Faces = ref("d")
	if _, err := BuildTrainingCaseFromDescriptor(run, candidate, descriptor,
		render, compiled, icc, model); err == nil {
		t.Fatal("mismatched descriptor faces accepted")
	}
	compiled.ApprovedSHA = strings.Repeat("f", 64)
	if _, err := BuildTrainingCaseEvidence(run, candidate, [12]float64{},
		render, compiled, icc, model); err == nil {
		t.Fatal("forged frozen SHA accepted")
	}
	approved.EffectiveDelta.EV = -1
	badBody, badSHA := body(approved)
	compiled.Approved, compiled.ApprovedBody, compiled.ApprovedSHA = approved, badBody, badSHA
	base.EffectiveDelta.EV = 1
	baseBody, baseSHA = body(base)
	compiled.Base, compiled.BaseBody, compiled.BaseSHA = base, baseBody, baseSHA
	if _, err := BuildTrainingCaseEvidence(run, candidate, [12]float64{},
		render, compiled, icc, model); err == nil {
		t.Fatal("out-of-range target accepted")
	}
}
