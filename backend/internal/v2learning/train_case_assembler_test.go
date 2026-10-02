package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type caseDocuments struct{ value TrainingEditingDocuments }

func (s caseDocuments) Load(context.Context, TrainingStart,
	TrainingCandidate) (TrainingEditingDocuments, error) {
	return s.value, nil
}

type caseRecipes struct{ value CompiledTrainingRecipes }

func (s caseRecipes) LoadPair(context.Context, auth.Identity, uuid.UUID,
	TrainingEditingDocuments, string, string) (CompiledTrainingRecipes,
	*v2draft.ResolvedCorrection, error) {
	return s.value, nil, nil
}

type caseImage struct{ value TrainingImage }

func (s caseImage) Load(context.Context, auth.Identity, uuid.UUID,
	v2input.SessionVersionBinding, string) (TrainingImage, error) {
	return s.value, nil
}
func (s caseImage) DiscoverICC(context.Context, auth.Identity, uuid.UUID,
	v2input.SessionVersionBinding) (string, error) {
	return s.value.Document.Color.ICCSHA256, nil
}

type caseDescriptor struct{ value TrainingDescriptor }

func (s caseDescriptor) Load(context.Context, auth.Identity, uuid.UUID,
	uuid.UUID, v2input.SessionVersionBinding, string,
	*v2draft.ResolvedCorrection) (TrainingDescriptor, error) {
	return s.value, nil
}

type caseFaces struct{ calls *int }

func (s caseFaces) Load(context.Context, auth.Identity, uuid.UUID,
	v2input.SessionVersionBinding, v2input.Ref) (int, error) {
	*s.calls++
	return 0, nil
}

type caseProtection struct{ ref v2input.Ref }

func (s caseProtection) SelectProtected(context.Context, auth.Identity,
	TrainingStart, TrainingCandidate, TrainingImage) (v2input.Ref,
	TrainingMask, error) {
	return s.ref, TrainingMask{}, nil
}

func TestTrainingCaseAssemblerBindsSourceLineageAndPinnedInputs(t *testing.T) {
	ref := func(letter string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat(letter, 64)}
	}
	working, style := ref("a"), ref("b")
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
		Source: working, CanvasHash: strings.Repeat("c", 64)}
	icc, model, lineage := strings.Repeat("d", 64), strings.Repeat("e", 64), strings.Repeat("f", 64)
	base := FrozenRecipe{SchemaVersion: 1, Source: binding,
		BaseStyle: style, Renderer: "v2.2", ICCLockSHA: icc,
		ModelLockSHA: model}
	approved := base
	approved.EffectiveDelta.EV = .25
	encode := func(recipe FrozenRecipe) ([]byte, string) {
		body, err := canonical.Marshal(recipe)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		return body, hex.EncodeToString(sum[:])
	}
	baseBody, baseSHA := encode(base)
	approvedBody, approvedSHA := encode(approved)
	compiled := CompiledTrainingRecipes{Base: base, Approved: approved,
		BaseBody: baseBody, BaseSHA: baseSHA,
		ApprovedBody: approvedBody, ApprovedSHA: approvedSHA}
	baseDoc, approvedDoc := ref("1"), ref("2")
	approval := uuid.New()
	candidate := TrainingCandidate{EventID: uuid.New(), SessionID: uuid.New(),
		AssetVersionID:   binding.AssetVersionID,
		SourceContentSHA: working.SHA256, SourceLineageSHA: lineage,
		BaseDocumentID: uuid.New(), BaseArtifactID: baseDoc.ArtifactID,
		BaseSHA: baseDoc.SHA256, ApprovedDocumentID: uuid.New(),
		ApprovedArtifactID: approvedDoc.ArtifactID,
		ApprovedSHA:        approvedDoc.SHA256, ApprovalID: &approval,
		ApprovedAt: time.Now().UTC(), ConsentID: uuid.New(), ConsentEpoch: 1,
		ChangedPaths: []string{"/ev"}, ConsentLive: true,
		SourceReady: true, BaseReady: true, ApprovedReady: true}
	documents := TrainingEditingDocuments{BaseDocumentID: candidate.BaseDocumentID,
		ApprovedDocumentID: candidate.ApprovedDocumentID,
		Binding:            binding, BaseStyle: style,
		ApprovedBody: []byte(`{}`)}
	facesCalls := 0
	assembler := TrainingCaseAssembler{Documents: caseDocuments{documents},
		Recipes: caseRecipes{compiled},
		Images: caseImage{TrainingImage{Document: review.WorkingImage{
			Canvas: review.Canvas{SourceSHA256: lineage},
			Color:  review.ColorSpace{ICCSHA256: icc}}, PixelsCount: 1}},
		Descriptors: caseDescriptor{TrainingDescriptor{Faces: ref("3"),
			Shard: ref("4"), AnalysisID: uuid.New()}},
		Faces:      caseFaces{&facesCalls},
		Protection: caseProtection{ref("5")}}
	start := TrainingStart{TenantID: uuid.New(), ProjectID: uuid.New(),
		ActorID: uuid.New(), AuthSessionID: uuid.New()}
	if discovered, err := assembler.DiscoverICC(context.Background(), start,
		candidate); err != nil || discovered != icc {
		t.Fatalf("case ICC discovery: %q, %v", discovered, err)
	}
	runID := uuid.New()
	got, err := assembler.Assemble(context.Background(), start, candidate,
		runID, icc, model)
	if err != nil || got.Source.Target[0] != .25 || facesCalls != 1 ||
		got.Resolved.Render.Protected.ArtifactID == uuid.Nil ||
		got.Resolved.Render.DescriptorShard.ArtifactID == uuid.Nil {
		t.Fatalf("case not assembled: %v", err)
	}
	candidate.SourceLineageSHA = strings.Repeat("0", 64)
	if _, err := assembler.Assemble(context.Background(), start, candidate,
		runID, icc, model); err == nil {
		t.Fatal("foreign source lineage accepted")
	}
}
