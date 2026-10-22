package v2learning

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/v2input"
)

type detailFixturePublisher struct {
	Bodies    []map[string]any
	Published map[string]v2input.Ref
	Used      map[string]bool
}

func (p *detailFixturePublisher) Publish(_ context.Context, task uuid.UUID, ordinal int, contract string, body []byte) (v2input.Ref, error) {
	name := "v2_input_manifest"
	if contract == "InputClosureChunk" {
		name = fmt.Sprintf("v2_input_chunk_%d", ordinal)
	}
	sum := sha256.Sum256(body)
	ref := v2input.Ref{ArtifactID: uuid.NewSHA1(task, []byte(name)), SHA256: hex.EncodeToString(sum[:])}
	if supplied, ok := p.Published[name]; ok {
		if supplied.ArtifactID == uuid.Nil || supplied.SHA256 != ref.SHA256 || p.Used[name] {
			return v2input.Ref{}, fmt.Errorf("invalid publication binding: %s", name)
		}
		ref = supplied
		p.Used[name] = true
	}
	p.Bodies = append(p.Bodies, map[string]any{"ref": ref, "contract": contract, "purpose": name, "body": json.RawMessage(body)})
	return ref, nil
}

// Work-only compiler exporter. It consumes actual fixture identities/material;
// it creates no SQL authority or model/physical qualification. Not acceptance.
func TestWriteV23DetailClosures(t *testing.T) {
	path := os.Getenv("PHOTO_TEST_V23_DETAIL_CLOSURE_REQUEST")
	output := os.Getenv("PHOTO_TEST_V23_DETAIL_CLOSURE_OUTPUT")
	if path == "" || output == "" {
		t.Skip("explicit fixture export required")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var input struct {
		Params    artifactjson.Ref
		Published map[string]v2input.Ref
		Stage     string
		Draft     TrainingDraftPrepared
		Policy    LearningPolicy
		Lease     LearningEvaluateLease
		Resolved  []LearningRenderInputs
	}
	if err = json.Unmarshal(raw, &input); err != nil {
		t.Fatal(err)
	}
	var body []byte
	var task uuid.UUID
	var closure FitClosure
	if input.Stage == "fit" {
		body, err = CompileFitParams(input.Draft, input.Policy)
		task, _ = v2input.TaskID(input.Draft.WorkflowID, "learning.fit", 0)
	} else if input.Stage == "evaluate" {
		body, err = CompileEvaluateParams(input.Lease, input.Draft)
		task, _ = v2input.TaskID(input.Draft.WorkflowID, "learning.evaluate", 0)
	} else {
		t.Fatal("closed stage required")
	}
	if err != nil || task == uuid.Nil {
		t.Fatal("compile params", err)
	}
	sum := sha256.Sum256(body)
	params := input.Params
	if params.ID == uuid.Nil || params.SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatal("published params reference does not match compiled body")
	}
	if input.Stage == "fit" {
		closure, err = CompileFitClosure(input.Draft, input.Policy, params)
	} else {
		closure, err = CompileEvaluateClosure(input.Lease, input.Draft, params, input.Resolved)
	}
	if err != nil {
		t.Fatal("compile domain closure", err)
	}
	ctx := context.Background()
	cursor, err := closure.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &detailFixturePublisher{Published: input.Published, Used: map[string]bool{}}
	built, err := v2input.BuildInputClosure(ctx, task, cursor, publisher)
	if err != nil {
		t.Fatal(err)
	}
	if len(publisher.Used) != len(input.Published) {
		t.Fatal("unused publication binding")
	}
	cursor, err = closure.Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	entries := []v2input.Entry{}
	for {
		entry, more, e := cursor.Next(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if !more {
			break
		}
		entries = append(entries, entry)
	}
	result := map[string]any{"task": task, "params": params, "params_body": json.RawMessage(body), "input_digest": closure.InputDigest, "profile": json.RawMessage(closure.Profile), "entries": entries, "built": built, "transport_bodies": publisher.Bodies, "direct": closure.Direct}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(output, encoded, 0600); err != nil {
		t.Fatal(err)
	}
}
