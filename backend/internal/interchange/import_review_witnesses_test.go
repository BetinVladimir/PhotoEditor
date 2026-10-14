package interchange

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestImportReviewWitnessesFreezeCurrentDecision(t *testing.T) {
	ref := func(name string) v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte(name))}
	}
	proposal, base, label := ref("proposal"), ref("base"), ref("label")
	id := rawSHA([]byte("candidate"))
	w := ImportReviewWitness{Entry: uuid.New(), Target: uuid.New(), Kind: "defects", Proposal: proposal, Base: base, LabelMap: &label, Epoch: 3, Decision: "accept", Accepted: []string{id}, Rejected: []string{}}
	registry := &ImportReviewWitnesses{}
	if err := registry.Record(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	first, err := registry.Snapshot(context.Background())
	if err != nil || len(first) != 1 {
		t.Fatal("missing witness", err)
	}
	w.Accepted[0] = rawSHA([]byte("changed"))
	first[0].Accepted[0] = rawSHA([]byte("changed again"))
	second, err := registry.Snapshot(context.Background())
	if err != nil || second[0].Accepted[0] != id {
		t.Fatal("witness aliased mutable input", err)
	}
	changed := second[0]
	changed.Epoch++
	if err := registry.Record(context.Background(), changed); err == nil {
		t.Fatal("conflicting epoch accepted")
	}
}
