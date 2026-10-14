package v2draft

import (
	"bytes"
	"sort"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestGlareDonorCandidatePageRequiresMonotonicScopedRows(t *testing.T) {
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	query := GlareDonorCandidateQuery{Shoot: uuid.New(), Snapshot: uuid.New(),
		Base:              v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: sha},
		SourceObservation: uuid.New()}
	queryRaw, err := canonical.Marshal(query)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeGlareDonorCandidateQuery(queryRaw); err != nil {
		t.Fatal(err)
	}
	first := GlareDonorCandidate{Observation: uuid.New(), Asset: uuid.New(), Version: uuid.New(), Source: ref(), Faces: ref()}
	page := GlareDonorCandidatePage{SourceFaces: ref(), Items: []GlareDonorCandidate{first}}
	pageRaw, err := canonical.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeGlareDonorCandidatePage(pageRaw, query); err != nil {
		t.Fatal(err)
	}
	page.Items[0].Version = query.Base.AssetVersionID
	pageRaw, err = canonical.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeGlareDonorCandidatePage(pageRaw, query); err != v2input.ErrInvalid {
		t.Fatal("base version offered as donor", err)
	}
}

func TestGlareCandidatePagesRequireExactContinuationCursor(t *testing.T) {
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	ids := make([]uuid.UUID, 100)
	for i := range ids {
		ids[i] = uuid.New()
	}
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })
	last, wrong := ids[99], uuid.New()
	if wrong == last {
		wrong = uuid.New()
	}
	base := v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref(), CanvasHash: sha}
	donorQuery := GlareDonorCandidateQuery{Shoot: uuid.New(), Snapshot: uuid.New(), Base: base, SourceObservation: uuid.New()}
	sourceQuery := GlareSourceCandidateQuery{Shoot: donorQuery.Shoot, Snapshot: donorQuery.Snapshot, Base: base}
	maskQuery := GlareMaskPairQuery{Snapshot: donorQuery.Snapshot, Base: base}
	donors := GlareDonorCandidatePage{SourceFaces: ref(), Items: make([]GlareDonorCandidate, 100), Next: &last}
	sources := GlareSourceCandidatePage{Items: make([]GlareSourceCandidate, 100), Next: &last}
	masks := GlareMaskPairPage{Items: make([]GlareMaskPair, 100), Next: &last}
	for i, id := range ids {
		donors.Items[i] = GlareDonorCandidate{Observation: id, Asset: uuid.New(), Version: uuid.New(), Source: ref(), Faces: ref()}
		sources.Items[i] = GlareSourceCandidate{Observation: id, Faces: ref()}
		masks.Items[i] = GlareMaskPair{GlassesMask: v2input.Ref{ArtifactID: id, SHA256: sha}, Protected: ref(), TargetPixel: ref(), ProtectedPixel: ref()}
	}
	check := func(wantValid bool) {
		t.Helper()
		body, _ := canonical.Marshal(donors)
		_, donorErr := decodeGlareDonorCandidatePage(body, donorQuery)
		body, _ = canonical.Marshal(sources)
		_, sourceErr := decodeGlareSourceCandidatePage(body, sourceQuery)
		body, _ = canonical.Marshal(masks)
		_, maskErr := decodeGlareMaskPairPage(body, maskQuery)
		for _, err := range []error{donorErr, sourceErr, maskErr} {
			if wantValid && err != nil || !wantValid && err != v2input.ErrInvalid {
				t.Fatal("continuation cursor binding", err)
			}
		}
	}
	check(true)
	donors.Next, sources.Next, masks.Next = &wrong, &wrong, &wrong
	check(false)
	donors.Items, sources.Items, masks.Items = donors.Items[:1], sources.Items[:1], masks.Items[:1]
	donors.Next, sources.Next, masks.Next = &ids[0], &ids[0], &ids[0]
	check(false)
}

func TestGlareSourceCandidatePageRejectsDuplicateObservations(t *testing.T) {
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	query := GlareSourceCandidateQuery{Shoot: uuid.New(), Snapshot: uuid.New(),
		Base: v2input.SessionVersionBinding{AssetVersionID: uuid.New(), Source: ref, CanvasHash: sha}}
	body, err := canonical.Marshal(query)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeGlareSourceCandidateQuery(body); err != nil {
		t.Fatal(err)
	}
	item := GlareSourceCandidate{Observation: uuid.New(), Faces: ref}
	page := GlareSourceCandidatePage{Items: []GlareSourceCandidate{item}}
	body, err = canonical.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeGlareSourceCandidatePage(body, query); err != nil {
		t.Fatal(err)
	}
	page.Items = append(page.Items, item)
	body, err = canonical.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeGlareSourceCandidatePage(body, query); err != v2input.ErrInvalid {
		t.Fatal("duplicate source observation accepted", err)
	}
}

func TestGlareMaskPairPageRejectsMixedProducerRoots(t *testing.T) {
	sha := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	ref := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: sha} }
	query := GlareMaskPairQuery{Snapshot: uuid.New(), Base: v2input.SessionVersionBinding{
		AssetVersionID: uuid.New(), Source: ref(), CanvasHash: sha}}
	body, err := canonical.Marshal(query)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeGlareMaskPairQuery(body); err != nil {
		t.Fatal(err)
	}
	pair := GlareMaskPair{GlassesMask: ref(), Protected: ref(), TargetPixel: ref(), ProtectedPixel: ref()}
	page := GlareMaskPairPage{Items: []GlareMaskPair{pair}}
	body, err = canonical.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeGlareMaskPairPage(body, query); err != nil {
		t.Fatal(err)
	}
	page.Items[0].ProtectedPixel = pair.TargetPixel
	body, err = canonical.Marshal(page)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeGlareMaskPairPage(body, query); err != v2input.ErrInvalid {
		t.Fatal("aliased mask pixels accepted", err)
	}
}
