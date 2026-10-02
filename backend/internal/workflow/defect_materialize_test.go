package workflow

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/review"
)

type documentPublisher struct {
	calls      []artifactjson.Scope
	bodies     [][]byte
	failParams bool
}

func (p *documentPublisher) Publish(_ context.Context, s artifactjson.Scope, b []byte) (artifactjson.Ref, error) {
	p.calls = append(p.calls, s)
	p.bodies = append(p.bodies, append([]byte(nil), b...))
	if p.failParams && s.Purpose == "task_params" {
		return artifactjson.Ref{}, errors.New("params upload unavailable")
	}
	h := sha256.Sum256(b)
	return artifactjson.Ref{ID: uuid.NewSHA1(s.Owner, []byte(s.Purpose)), SHA256: hex.EncodeToString(h[:])}, nil
}
func expansionFixture(t *testing.T) DefectExpansionInput {
	t.Helper()
	read := func(name string) []byte {
		b, e := os.ReadFile("../review/testdata/" + name)
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	raw := read("defect-proposal-python.json")
	var d struct {
		Image  review.ArtifactRef `json:"image"`
		Pixels review.ArtifactRef `json:"source_pixels"`
		Canvas review.Canvas      `json:"canvas"`
		Chain  json.RawMessage    `json:"transform_chain"`
	}
	if err := json.Unmarshal(raw, &d); err != nil {
		t.Fatal(err)
	}
	var selection review.DefectSelection
	if err := json.Unmarshal(read("defect-selection-python.json"), &selection); err != nil {
		t.Fatal(err)
	}
	p, err := review.ReadDefectProposal(context.Background(), bytes.NewReader(raw), selection.Proposal, review.DefectSource{Image: d.Image, Pixels: d.Pixels, Canvas: d.Canvas, TransformChain: d.Chain})
	if err != nil {
		t.Fatal(err)
	}
	var params struct {
		Resolved  review.ArtifactRef    `json:"resolved"`
		Protected review.ProtectionMask `json:"protected"`
	}
	if err = json.Unmarshal(read("defect-repair-params-python.json"), &params); err != nil {
		t.Fatal(err)
	}
	return DefectExpansionInput{TenantID: uuid.New(), RunID: uuid.New(), ExpansionID: uuid.New(), ReviewID: uuid.New(), Route: "repair", Proposal: p, Selection: selection, Resolved: params.Resolved,
		ResolvedImage: review.ArtifactRef{ID: uuid.New(), SHA256: d.Image.SHA256}, Protection: params.Protected}
}
func TestDefectBranchPublishesSelectionThenParams(t *testing.T) {
	input := expansionFixture(t)
	publisher := new(documentPublisher)
	branch, err := MaterializeDefectBranch(context.Background(), publisher, input)
	if err != nil {
		t.Fatal(err)
	}
	if len(publisher.calls) != 2 || publisher.calls[0].Purpose != "review_selection" || publisher.calls[0].Owner != input.ReviewID || publisher.calls[1].Purpose != "task_params" || publisher.calls[1].Owner != input.ExpansionID {
		t.Fatal("wrong publication scopes")
	}
	if branch.Params == nil || branch.Selection == nil || branch.BaseImage != nil || branch.Route != "repair" {
		t.Fatal("incomplete branch")
	}
	found := false
	for _, ref := range branch.Inputs {
		if ref == input.ResolvedImage {
			found = true
		}
	}
	if !found {
		t.Fatal("resolved origin undeclared")
	}
	var params struct {
		Review review.ArtifactRef `json:"review"`
	}
	if err = json.Unmarshal(publisher.bodies[1], &params); err != nil || params.Review != *branch.Selection {
		t.Fatal("params did not bind published selection", err)
	}
}
func TestDefectMaterializationRetryAndPassThrough(t *testing.T) {
	input := expansionFixture(t)
	publisher := &documentPublisher{failParams: true}
	if branch, err := MaterializeDefectBranch(context.Background(), publisher, input); err == nil || branch.Params != nil {
		t.Fatal("partial publication returned runnable branch")
	}
	first := append([]byte(nil), publisher.bodies[1]...)
	publisher.failParams = false
	if _, err := MaterializeDefectBranch(context.Background(), publisher, input); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, publisher.bodies[3]) {
		t.Fatal("retry changed params")
	}
	for _, route := range []string{"base", "skip"} {
		input.Route = route
		input.Selection.Accepted = []string{}
		p := new(documentPublisher)
		branch, err := MaterializeDefectBranch(context.Background(), p, input)
		if err != nil || len(p.calls) != 0 || branch.Params != nil || branch.Selection != nil {
			t.Fatal("pass through published work", err)
		}
		if (branch.BaseImage != nil) != (route == "base") {
			t.Fatal("incorrect base route")
		}
	}
}
func TestInvalidRepairHasNoPublication(t *testing.T) {
	input := expansionFixture(t)
	input.Protection.Protected = false
	p := new(documentPublisher)
	if _, err := MaterializeDefectBranch(context.Background(), p, input); err == nil || len(p.calls) != 0 {
		t.Fatal("invalid input caused artifact side effects")
	}
}

func TestInvalidBaseSelectionHasNoPublication(t *testing.T) {
	input := expansionFixture(t)
	input.Route = "base"
	input.Selection.Accepted = []string{}
	input.Selection.Rejected = []string{input.Resolved.SHA256}
	p := new(documentPublisher)
	if _, err := MaterializeDefectBranch(context.Background(), p, input); err == nil || len(p.calls) != 0 {
		t.Fatal("unknown base decision accepted")
	}
}
