package review

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func proposalFixture(t *testing.T) ([]byte, ArtifactRef, DefectSource) {
	t.Helper()
	raw, err := os.ReadFile("testdata/defect-proposal-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc proposalDocument
	if err = json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return raw, proposalRef(raw), DefectSource{doc.Image, doc.SourcePixels, doc.Canvas, doc.TransformChain}
}
func proposalRef(raw []byte) ArtifactRef {
	h := sha256.Sum256(raw)
	return ArtifactRef{uuid.MustParse("00000000-0000-0000-0000-000000000004"), hex.EncodeToString(h[:])}
}

func TestPythonProposalIdentity(t *testing.T) {
	raw, ref, source := proposalFixture(t)
	p, err := ReadDefectProposal(context.Background(), bytes.NewReader(raw), ref, source)
	if err != nil {
		t.Fatal(err)
	}
	s, actor, req := fixture()
	s.Proposal = ref
	s.Image = source.Image
	s.LabelMap = p.document.LabelMap
	s, err = p.Bind(s)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.CandidateIDs) != 1 {
		t.Fatal("missing candidate")
	}
	req.ExpectedRevision = s.Revision
	req.CandidateHash = ref.SHA256
	req.Accepted = s.CandidateIDs
	plan, err := PlanDefects(s, actor, req)
	if err != nil || plan.Route != "repair" {
		t.Fatal("verified proposal cannot be selected", err)
	}
	s.CandidateIDs[0] = "modified"
	next, err := p.Bind(s)
	if err != nil || next.CandidateIDs[0] == "modified" {
		t.Fatal("catalog aliases caller memory", err)
	}
	s.Proposal.ID = uuid.New()
	if _, err = p.Bind(s); err == nil {
		t.Fatal("accepted other proposal")
	}
}

func TestProposalRejectsContentAndGeometryChanges(t *testing.T) {
	raw, ref, source := proposalFixture(t)
	for _, change := range []func(map[string]any){
		func(d map[string]any) { d["version"] = "next" },
		func(d map[string]any) { d["extra"] = 1 },
		func(d map[string]any) { d["transform_chain"] = map[string]any{} },
		func(d map[string]any) { d["candidates"] = nil },
		func(d map[string]any) { c := d["candidates"].([]any)[0].(map[string]any); c["diameter"] = 3.0 },
		func(d map[string]any) { c := d["candidates"].([]any)[0].(map[string]any); c["score"] = 1.1 },
		func(d map[string]any) { c := d["candidates"].([]any)[0].(map[string]any); delete(c, "decision") },
		func(d map[string]any) { c := d["candidates"].([]any)[0].(map[string]any); c["score"] = nil },
		func(d map[string]any) { a := d["candidates"].([]any); d["candidates"] = append(a, a[0]) },
	} {
		var d map[string]any
		json.Unmarshal(raw, &d)
		change(d)
		b, _ := json.Marshal(d)
		if _, err := ReadDefectProposal(context.Background(), bytes.NewReader(b), proposalRef(b), source); err == nil {
			t.Fatalf("accepted invalid proposal: %s", b)
		}
	}
	if _, err := ReadDefectProposal(context.Background(), bytes.NewReader(append(raw, ' ')), ref, source); err == nil {
		t.Fatal("did not hash original bytes")
	}
	source.Pixels.SHA256 = strings.Repeat("e", 64)
	if _, err := ReadDefectProposal(context.Background(), bytes.NewReader(raw), ref, source); err == nil {
		t.Fatal("accepted stale pixels")
	}
}

func TestProposalBoundedAndCancelled(t *testing.T) {
	raw, ref, source := proposalFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ReadDefectProposal(ctx, bytes.NewReader(raw), ref, source); err != context.Canceled {
		t.Fatal(err)
	}
	reader := bytes.NewReader(bytes.Repeat([]byte(" "), 2*1048576))
	if _, err := ReadDefectProposal(context.Background(), reader, ref, source); err == nil {
		t.Fatal("oversized proposal accepted")
	}
	if reader.Len() != 1048575 {
		t.Fatal("read beyond limit", reader.Len())
	}
	duplicate := bytes.Replace(raw, []byte(`"version":"defect_candidates_v1"`), []byte(`"version":"defect_candidates_v1","version":"defect_candidates_v1"`), 1)
	if _, err := ReadDefectProposal(context.Background(), bytes.NewReader(duplicate), proposalRef(duplicate), source); err == nil {
		t.Fatal("duplicate fields accepted")
	}
}
