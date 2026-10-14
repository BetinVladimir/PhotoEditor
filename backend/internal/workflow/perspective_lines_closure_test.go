package workflow

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"testing"
)

type linesClosurePublisher struct {
	alias   uuid.UUID
	corrupt bool
	calls   int
}

func (p *linesClosurePublisher) Publish(ctx context.Context, task uuid.UUID, ordinal int, contract string, raw []byte) (v2input.Ref, error) {
	p.calls++
	id := uuid.NewSHA1(task, []byte(contract+":"+strconv.Itoa(ordinal)))
	if p.alias != uuid.Nil {
		id = p.alias
	}
	if p.corrupt {
		raw[0] = 'x'
	}
	return v2input.Ref{ArtifactID: id, SHA256: renderDerivativeBytesSHA(raw)}, nil
}
func TestPerspectiveLinesClosurePinsSortedEntriesAndOriginalControlBytes(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, true)
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	publisher := &linesClosurePublisher{}
	closure, err := BuildPerspectiveLinesClosure(context.Background(), plan, params, publisher)
	if err != nil {
		t.Fatal(err)
	}
	built := closure.Built()
	docs := closure.Documents()
	if publisher.calls != 2 || built.Count != 4 || len(built.Chunks) != 1 || len(docs) != 2 {
		t.Fatal("incomplete closure")
	}
	var manifest v2input.Manifest
	if json.Unmarshal(docs[built.Manifest.ArtifactID], &manifest) != nil || manifest.TaskID != plan.Task() || manifest.EntryCount != 4 || manifest.EntriesSHA != built.SHA256 {
		t.Fatal("wrong manifest")
	}
	var chunk v2input.Chunk
	if json.Unmarshal(docs[built.Chunks[0].ArtifactID], &chunk) != nil || len(chunk.Entries) != 4 {
		t.Fatal("wrong chunk")
	}
	entries, err := canonical.Marshal(closure.Entries())
	if err != nil {
		t.Fatal(err)
	}
	if renderDerivativeBytesSHA(entries) != built.SHA256 {
		t.Fatal("entry digest substitution")
	}
	for id, body := range docs {
		original := bytes.Clone(body)
		body[0] = 'x'
		if !bytes.Equal(closure.Documents()[id], original) {
			t.Fatal("mutable document exposure")
		}
	}
	exposed := closure.Entries()
	exposed[0].Contract = "forged"
	built.Chunks[0].ArtifactID = uuid.New()
	if closure.Entries()[0].Contract == "forged" || closure.Built().Chunks[0].ArtifactID == built.Chunks[0].ArtifactID {
		t.Fatal("mutable closure exposure")
	}
}
func TestPerspectiveLinesClosureRejectsMetadataAliasAndChangedPublication(t *testing.T) {
	scope, raw, icc := perspectiveLinesFixture(t, false)
	plan, err := CompilePerspectiveLinesPlan(scope, raw, icc)
	if err != nil {
		t.Fatal(err)
	}
	params := review.ArtifactRef{ID: uuid.New(), SHA256: renderDerivativeBytesSHA(plan.Params())}
	for _, publisher := range []*linesClosurePublisher{{alias: params.ID}, {alias: scope.Base.Source.ArtifactID}, {alias: uuid.New()}, {corrupt: true}} {
		if _, err := BuildPerspectiveLinesClosure(context.Background(), plan, params, publisher); err == nil {
			t.Fatal("invalid transport accepted")
		}
	}
}
func TestPerspectiveLinesClosurePreflightAndCancellationPrecedePublisher(t *testing.T) {
	publisher := &linesClosurePublisher{}
	if _, err := BuildPerspectiveLinesClosure(context.Background(), PerspectiveLinesPlan{}, review.ArtifactRef{}, publisher); err == nil {
		t.Fatal("uncompiled plan accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildPerspectiveLinesClosure(ctx, PerspectiveLinesPlan{}, review.ArtifactRef{}, publisher); err != context.Canceled {
		t.Fatal(err)
	}
	if publisher.calls != 0 {
		t.Fatal("publication before validation")
	}
}
