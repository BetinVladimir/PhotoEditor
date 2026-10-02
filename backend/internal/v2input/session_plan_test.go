package v2input

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCompileSessionDescriptors(t *testing.T) {
	workflow := uuid.New()
	for _, n := range []int{1, 128, 129, 5000} {
		plan, err := CompileSessionDescriptors(workflow, n)
		if err != nil || len(plan.Tasks) != (n+127)/128 {
			t.Fatalf("count %d: %v", n, err)
		}
		seen := 0
		for shard, task := range plan.Tasks {
			id, _ := TaskID(workflow, "session.describe", shard)
			if task.ID != id || len(task.Ordinals) == 0 || len(task.Ordinals) > 128 {
				t.Fatalf("invalid shard %d", shard)
			}
			for _, ordinal := range task.Ordinals {
				if ordinal != seen {
					t.Fatalf("unexpected ordinal %d, want %d", ordinal, seen)
				}
				seen++
			}
		}
		if seen != n {
			t.Fatalf("covered %d, want %d", seen, n)
		}
	}
	for _, n := range []int{0, -1, 5001} {
		if _, err := CompileSessionDescriptors(workflow, n); err == nil {
			t.Fatalf("accepted count %d", n)
		}
	}
	if _, err := CompileSessionDescriptors(uuid.Nil, 1); err == nil {
		t.Fatal("accepted nil workflow")
	}
}

func TestPublishSessionPlanParamsUsesExpansionScope(t *testing.T) {
	workflow := uuid.New()
	plan, err := CompileSessionDescriptors(workflow, 1)
	if err != nil {
		t.Fatal(err)
	}
	lease := SessionPlanLease{ExpansionID: uuid.New(), Token: uuid.New(),
		Until: time.Now().Add(time.Minute), TenantID: uuid.New(),
		RunID: uuid.New(), WorkflowID: workflow}
	results := []SessionShardResult{{TaskID: plan.Tasks[0].ID,
		Document: Ref{ArtifactID: uuid.New(), SHA256: string(bytes.Repeat([]byte{'a'}, 64))}}}
	contextRef := Ref{ArtifactID: uuid.New(), SHA256: string(bytes.Repeat([]byte{'b'}, 64))}
	docs := &scopedDocuments{}
	ref, err := PublishSessionPlanParams(context.Background(), docs, lease, plan,
		results, map[string]any{"schema_version": 1}, contextRef)
	if err != nil || !validRef(ref) || len(docs.scopes) != 1 {
		t.Fatalf("publication failed: %+v %v", ref, err)
	}
	scope := docs.scopes[0]
	if scope.Tenant != lease.TenantID || scope.Run != lease.RunID ||
		scope.Owner != lease.ExpansionID || scope.Purpose != "v2_session_plan_params" {
		t.Fatalf("wrong scope: %+v", scope)
	}
	results[0].TaskID = uuid.New()
	if _, err := PublishSessionPlanParams(context.Background(), docs, lease, plan,
		results, map[string]any{"schema_version": 1}, contextRef); err == nil || len(docs.scopes) != 1 {
		t.Fatal("published noncanonical shard")
	}
}

func TestCompileSessionPlanParamsRequiresCanonicalOrderedResults(t *testing.T) {
	workflow := uuid.New()
	plan, err := CompileSessionDescriptors(workflow, 129)
	if err != nil {
		t.Fatal(err)
	}
	results := []SessionShardResult{
		{TaskID: plan.Tasks[0].ID, Document: Ref{ArtifactID: uuid.New(), SHA256: string(bytes.Repeat([]byte{'a'}, 64))}},
		{TaskID: plan.Tasks[1].ID, Document: Ref{ArtifactID: uuid.New(), SHA256: string(bytes.Repeat([]byte{'b'}, 64))}},
	}
	context := Ref{ArtifactID: uuid.New(), SHA256: string(bytes.Repeat([]byte{'c'}, 64))}
	policy := map[string]any{"schema_version": 1}
	encoded, err := CompileSessionPlanParams(plan, results, policy, context)
	if err != nil || !bytes.Contains(encoded, []byte(`"shards"`)) {
		t.Fatalf("valid params: %s %v", encoded, err)
	}
	results[0], results[1] = results[1], results[0]
	if _, err := CompileSessionPlanParams(plan, results, policy, context); err == nil {
		t.Fatal("accepted reordered result")
	}
	results[0], results[1] = results[1], results[0]
	results[1].Document = results[0].Document
	if _, err := CompileSessionPlanParams(plan, results, policy, context); err == nil {
		t.Fatal("accepted duplicate descriptor")
	}
}
