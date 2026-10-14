package review

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestProposalFaceOriginalLandmarkBinding(t *testing.T) {
	raw, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	collection, err := os.ReadFile("testdata/face-landmarks-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var report proposalReportDocument
	var landmarks faceLandmarksDocument
	if decodeExact(raw, &report) != nil || decodeExact(collection, &landmarks) != nil {
		t.Fatal("fixtures")
	}
	source := DefectSource{Image: landmarks.Image, Canvas: landmarks.Landmarks[0].Canvas, TransformChain: landmarks.Landmarks[0].TransformChain}
	sum := sha256.Sum256(collection)
	ref := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	report.Params.Faces[0].Landmarks = ref
	body, err := canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	face, err := ResolveProposalFace(body, report.Proposal, source, 0, ref, collection)
	if err != nil || face.ObjectID != landmarks.Landmarks[0].ObjectID {
		t.Fatal(face, err)
	}
	for _, ordinal := range []int{-1, 1, 2} {
		if got, err := ResolveProposalFace(body, report.Proposal, source, ordinal, ref, collection); err == nil || got.ObjectID != uuid.Nil {
			t.Fatal("wrong ordinal/ref accepted", ordinal)
		}
	}
	wrong := ref
	wrong.ID = uuid.New()
	if got, err := ResolveProposalFace(body, report.Proposal, source, 0, wrong, collection); err == nil || got.ObjectID != uuid.Nil {
		t.Fatal("foreign document accepted")
	}
	if got, err := ResolveProposalFace(body, report.Proposal, source, 0, ref, append(append([]byte{}, collection...), ' ')); err == nil || got.ObjectID != uuid.Nil {
		t.Fatal("different original bytes accepted")
	}
	standalone, err := os.ReadFile("testdata/face68-python.json")
	if err != nil {
		t.Fatal(err)
	}
	sum = sha256.Sum256(standalone)
	ref.SHA256 = hex.EncodeToString(sum[:])
	report.Params.Faces[0].ObjectID = nil
	report.Params.Faces[0].Landmarks = ref
	body, _ = canonical.Marshal(report)
	if face, err := ResolveProposalFace(body, report.Proposal, source, 0, ref, standalone); err != nil || face.ObjectID != landmarks.Landmarks[0].ObjectID {
		t.Fatal("legacy standalone", face, err)
	}
	report.Faces[0].ObjectID = uuid.New()
	body, _ = canonical.Marshal(report)
	if got, err := ResolveProposalFace(body, report.Proposal, source, 0, ref, standalone); err == nil || got.ObjectID != uuid.Nil {
		t.Fatal("unreported resolved legacy object accepted")
	}
}

func TestProposalCompleteFaceSetAndCancellation(t *testing.T) {
	reportRaw, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	landmarkRaw, err := os.ReadFile("testdata/face-landmarks-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var report proposalReportDocument
	var collection faceLandmarksDocument
	if decodeExact(reportRaw, &report) != nil || decodeExact(landmarkRaw, &collection) != nil {
		t.Fatal("fixtures")
	}
	second := collection.Landmarks[0]
	second.ObjectID = *report.Params.Faces[1].ObjectID
	collection.Landmarks = append(collection.Landmarks, second)
	landmarkRaw, err = canonical.Marshal(collection)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(landmarkRaw)
	ref := ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	for i := range report.Params.Faces {
		report.Params.Faces[i].Landmarks = ref
	}
	reportRaw, err = canonical.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	source := DefectSource{Image: collection.Image, Canvas: second.Canvas, TransformChain: second.TransformChain}
	calls := 0
	loader := func(ctx context.Context, requested ArtifactRef) ([]byte, error) {
		calls++
		if requested != ref {
			return nil, errors.New("foreign ref")
		}
		return landmarkRaw, nil
	}
	faces, err := ResolveProposalFaces(context.Background(), reportRaw, report.Proposal, source, loader)
	if err != nil || len(faces) != 2 || calls != 2 || faces[0].ObjectID == faces[1].ObjectID {
		t.Fatal(faces, err, calls)
	}
	mutationReport := append([]byte(nil), reportRaw...)
	mutationSource := source
	mutationSource.TransformChain = append([]byte(nil), source.TransformChain...)
	mutationCalls := 0
	if faces, err := ResolveProposalFaces(context.Background(), mutationReport, report.Proposal, mutationSource, func(context.Context, ArtifactRef) ([]byte, error) {
		mutationCalls++
		for i := range mutationReport {
			mutationReport[i] = ' '
		}
		for i := range mutationSource.TransformChain {
			mutationSource.TransformChain[i] = ' '
		}
		return landmarkRaw, nil
	}); err != nil || len(faces) != 2 || mutationCalls != 2 {
		t.Fatal("loader changed owned report/source", faces, err)
	}
	calls = 0
	failure := errors.New("object unavailable")
	if faces, err := ResolveProposalFaces(context.Background(), reportRaw, report.Proposal, source, func(ctx context.Context, requested ArtifactRef) ([]byte, error) {
		calls++
		if calls == 2 {
			return nil, failure
		}
		return landmarkRaw, nil
	}); !errors.Is(err, failure) || faces != nil {
		t.Fatal("partial result", faces, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	if faces, err := ResolveProposalFaces(ctx, reportRaw, report.Proposal, source, func(ctx context.Context, requested ArtifactRef) ([]byte, error) { cancel(); return landmarkRaw, nil }); !errors.Is(err, context.Canceled) || faces != nil {
		t.Fatal("cancellation ignored", faces, err)
	}
	standalone, err := os.ReadFile("testdata/face68-python.json")
	if err != nil {
		t.Fatal(err)
	}
	sum = sha256.Sum256(standalone)
	// Distinct standalone refs both resolve to one object, while report IDs remain unique.
	for i := range report.Params.Faces {
		report.Params.Faces[i].ObjectID = nil
		report.Params.Faces[i].Skin.ObjectID = nil
		report.Params.Faces[i].Landmarks = ArtifactRef{ID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
	}
	reportRaw, _ = canonical.Marshal(report)
	calls = 0
	if faces, err := ResolveProposalFaces(context.Background(), reportRaw, report.Proposal, source, func(context.Context, ArtifactRef) ([]byte, error) { calls++; return standalone, nil }); err == nil || faces != nil || calls != 2 {
		t.Fatal("duplicate resolved legacy objects accepted", faces, err, calls)
	}
	if faces, err := ResolveProposalFaces(context.Background(), reportRaw, report.Proposal, source, nil); err == nil || faces != nil {
		t.Fatal("nil loader accepted")
	}
}

func TestProposalLandmarkDependenciesCompleteUniqueAndOwned(t *testing.T) {
	raw, err := os.ReadFile("testdata/proposal-report-multiple-faces-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var report proposalReportDocument
	if decodeExact(raw, &report) != nil {
		t.Fatal("fixture")
	}
	source := DefectSource{Image: report.Params.Image, Canvas: report.Protection.Canvas, TransformChain: report.Protection.TransformChain}
	refs, err := ProposalLandmarkRefs(raw, report.Proposal, source)
	if err != nil || len(refs) != 1 || refs[0] != report.Params.Faces[0].Landmarks {
		t.Fatal("shared collection", refs, err)
	}
	report.Params.Faces[1].Landmarks.ID = uuid.New()
	body, _ := canonical.Marshal(report)
	refs, err = ProposalLandmarkRefs(body, report.Proposal, source)
	if err != nil || len(refs) != 2 || refs[0] != report.Params.Faces[0].Landmarks || refs[1] != report.Params.Faces[1].Landmarks {
		t.Fatal("missing dependency", refs, err)
	}
	for i := range body {
		body[i] = ' '
	}
	if refs[1] != report.Params.Faces[1].Landmarks {
		t.Fatal("borrowed raw refs")
	}
	report.Params.Faces[1].Landmarks.ID = report.Params.Faces[0].Landmarks.ID
	report.Params.Faces[1].Landmarks.SHA256 = strings.Repeat("a", 64)
	body, _ = canonical.Marshal(report)
	if refs, err := ProposalLandmarkRefs(body, report.Proposal, source); err == nil || refs != nil {
		t.Fatal("contradictory digest accepted", refs, err)
	}
	report.Params.Faces = []proposalFaceInput{}
	report.Faces = []proposalFaceEvidence{}
	report.Warnings = []string{"no_faces"}
	body, _ = canonical.Marshal(report)
	if refs, err := ProposalLandmarkRefs(body, report.Proposal, source); err != nil || len(refs) != 0 {
		t.Fatal("empty dependencies", refs, err)
	}
}
