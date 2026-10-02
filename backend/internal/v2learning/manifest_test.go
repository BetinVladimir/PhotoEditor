package v2learning

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestCompileTrainingManifestPinsSplitAndRenderCases(t *testing.T) {
	rows := trainingCorpus()
	split, err := FreezeTrainingSplit(rows)
	if err != nil {
		t.Fatal(err)
	}
	ref := func(letter string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat(letter, 64)}
	}
	consentID := uuid.New()
	cases := make(map[uuid.UUID]ResolvedTrainingCase, len(rows))
	for _, source := range rows {
		base, approved, working := ref("b"), ref("c"), ref("d")
		approval := uuid.New()
		candidate := TrainingCandidate{EventID: source.EventID,
			SessionID: source.SessionID, AssetVersionID: uuid.New(),
			SourceContentSHA: source.SourceContentSHA,
			SourceLineageSHA: source.SourceLineageSHA,
			BaseDocumentID:   uuid.New(), BaseArtifactID: base.ArtifactID,
			BaseSHA: base.SHA256, ApprovedDocumentID: uuid.New(),
			ApprovedArtifactID: approved.ArtifactID,
			ApprovedSHA:        approved.SHA256, ApprovalID: &approval,
			ApprovedAt: source.ApprovedAt, ConsentID: consentID,
			ConsentEpoch: 1, ChangedPaths: []string{"/ev"},
			ConsentLive: true, SourceReady: true, BaseReady: true,
			ApprovedReady: true}
		render := TrainingRenderCase{
			Binding: v2input.SessionVersionBinding{AssetVersionID: candidate.AssetVersionID,
				Source: working, CanvasHash: strings.Repeat("e", 64)},
			WorkingImage: working, BaseRecipe: base, ApprovedRecipe: approved,
			BaseDocument: base, ApprovedDocument: approved,
			DescriptorShard: ref("8"), DescriptorAnalysisID: uuid.New(),
			DescriptorOrdinal: 0,
			Protected:         ref("f"), Faces: ref("0"), ApprovalID: approval,
			SourceLineageSHA: source.SourceLineageSHA}
		cases[source.EventID] = ResolvedTrainingCase{
			Candidate: candidate, Render: render}
	}
	context := ManifestContext{ProfileID: uuid.New(), ProjectID: uuid.New(),
		Mode: "photographer", PolicySHA: strings.Repeat("1", 64),
		Baseline: ref("2"), ICCLockSHA: strings.Repeat("3", 64),
		ModelLockSHA: strings.Repeat("4", 64), Calibration: ref("5")}
	manifest, body, digest, err := CompileTrainingManifest(context, split, cases)
	if err != nil {
		t.Fatal(err)
	}
	check := sha256.Sum256(body)
	if digest != hex.EncodeToString(check[:]) ||
		len(manifest.Examples) != 60 || len(manifest.Consents) != 1 ||
		manifest.Consents[0].Epoch != "1" ||
		manifest.RendererVersion != "v2.2" {
		t.Fatalf("unexpected manifest: %+v", manifest)
	}
	if path := os.Getenv("PHOTO_TEST_MANIFEST_OUTPUT"); path != "" {
		if err := os.WriteFile(path, body, 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, again, _, err := CompileTrainingManifest(context, split, cases)
	if err != nil || !bytes.Equal(body, again) {
		t.Fatal("manifest serialization is not deterministic", err)
	}
	first := split.Examples[0].Source.EventID
	bad := cases[first]
	effective := ref("6")
	bad.Render.EffectiveMask = &effective
	cases[first] = bad
	if _, _, _, err := CompileTrainingManifest(context, split, cases); err == nil {
		t.Fatal("accepted unpaired identity evidence")
	}
	consentSnapshot := ref("7")
	bad.Render.ConsentSnapshot = &consentSnapshot
	cases[first] = bad
	if _, _, _, err := CompileTrainingManifest(context, split, cases); err != nil {
		t.Fatal("rejected paired identity evidence", err)
	}
	bad.Render.Binding.AssetVersionID = uuid.New()
	cases[first] = bad
	if _, _, _, err := CompileTrainingManifest(context, split, cases); err == nil {
		t.Fatal("accepted cross-version RenderCase")
	}
	bad.Render.Binding.AssetVersionID = bad.Candidate.AssetVersionID
	bad.Candidate.ChangedPaths = []string{"/crop"}
	cases[first] = bad
	if _, _, _, err := CompileTrainingManifest(context, split, cases); err == nil {
		t.Fatal("accepted nonparametric training example")
	}
	bad.Candidate.ChangedPaths = []string{"/ev"}
	cases[first] = bad
	split.Examples[0].Split = "train"
	if _, _, _, err := CompileTrainingManifest(context, split, cases); err == nil {
		t.Fatal("accepted forged holdout assignment")
	}
}
