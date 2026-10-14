package v2optical

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
)

type MultiDraftSnapshot struct {
	Tenant         uuid.UUID       `json:"tenant_id"`
	Project        uuid.UUID       `json:"project_id"`
	Actor          uuid.UUID       `json:"actor_id"`
	Session        uuid.UUID       `json:"auth_session_id"`
	SnapshotID     uuid.UUID       `json:"snapshot_id"`
	Operation      string          `json:"operation"`
	Run            uuid.UUID       `json:"run_id"`
	Workflow       uuid.UUID       `json:"workflow_id"`
	Task           uuid.UUID       `json:"task_id"`
	ParamsDocument uuid.UUID       `json:"params_document_id"`
	ParamsJCS      string          `json:"params_jcs"`
	ParamsSHA      string          `json:"params_sha"`
	SourcesJCS     []string        `json:"sources_jcs"`
	Profile        json.RawMessage `json:"execution_profile"`
	ReservedBytes  int64           `json:"reserved_bytes"`
}

// MultiDraft owns immutable recovery bytes; it grants no admission authority.
type MultiDraft struct{ raw string }

func (d MultiDraft) Snapshot() (MultiDraftSnapshot, error) {
	var s MultiDraftSnapshot
	if len(d.raw) < 2 || len(d.raw) > 40*1024*1024 || json.Unmarshal([]byte(d.raw), &s) != nil {
		return s, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(s)
	if err != nil || !bytes.Equal(body, []byte(d.raw)) {
		return MultiDraftSnapshot{}, v2input.ErrInvalid
	}
	return s, nil
}
func CompileMultiDraft(ctx context.Context, who auth.Identity, tenant, project, snapshot uuid.UUID, key, operation string,
	paramsRaw []byte, originals [][]byte, qualified VerifiedMultiQualification, capacity MultiResourceEstimate) (MultiDraft, error) {
	var zero MultiDraft
	if tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || !idempotency.ValidKey(key) {
		return zero, v2input.ErrInvalid
	}
	plan, err := CompileMultiTaskPlan(ctx, operation, paramsRaw, originals, qualified, capacity)
	if err != nil {
		return zero, err
	}
	run := multiRunID(tenant, project, who.UserID, operation, key)
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return zero, err
	}
	task, err := v2input.TaskID(workflow, operation, 0)
	if err != nil {
		return zero, err
	}
	sources := make([]string, len(originals))
	for i, raw := range originals {
		sources[i] = string(raw)
	}
	s := MultiDraftSnapshot{Tenant: tenant, Project: project, Actor: who.UserID, Session: who.SessionID, SnapshotID: snapshot,
		Operation: operation, Run: run, Workflow: workflow, Task: task, ParamsDocument: uuid.NewSHA1(task, []byte("params")),
		ParamsJCS: string(plan.Params), ParamsSHA: plan.ParamsSHA, SourcesJCS: sources, Profile: plan.Profile, ReservedBytes: plan.ReservedBytes}
	body, err := canonical.Marshal(s)
	if err != nil || len(body) > 40*1024*1024 {
		return zero, v2input.ErrInvalid
	}
	return MultiDraft{raw: string(body)}, ctx.Err()
}
func (d MultiDraft) Descriptor(ctx context.Context, qualified VerifiedMultiQualification, publishedParams v2input.Ref) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s, err := d.Snapshot()
	if err != nil {
		return nil, err
	}
	if s.Tenant == uuid.Nil || s.Project == uuid.Nil || s.Actor == uuid.Nil || s.Session == uuid.Nil || s.SnapshotID == uuid.Nil || s.Run == uuid.Nil || publishedParams.ArtifactID != s.ParamsDocument || publishedParams.SHA256 != s.ParamsSHA {
		return nil, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(s.Run)
	if err != nil || workflow != s.Workflow {
		return nil, v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, s.Operation, 0)
	if err != nil || task != s.Task || uuid.NewSHA1(task, []byte("params")) != s.ParamsDocument {
		return nil, v2input.ErrInvalid
	}
	originals := make([][]byte, len(s.SourcesJCS))
	for i, raw := range s.SourcesJCS {
		originals[i] = []byte(raw)
	}
	var header struct {
		Resources MultiResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(s.Profile, &header) != nil {
		return nil, v2input.ErrInvalid
	}
	plan, err := CompileMultiTaskPlan(ctx, s.Operation, []byte(s.ParamsJCS), originals, qualified, header.Resources)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(plan.Profile, s.Profile) || plan.ParamsSHA != s.ParamsSHA || plan.ReservedBytes != s.ReservedBytes {
		return nil, v2input.ErrInvalid
	}
	entries, _, err := MultiTaskInputEntries(ctx, plan, originals, qualified, publishedParams)
	if err != nil {
		return nil, err
	}
	digest, err := canonical.Hash(struct {
		Task      uuid.UUID       `json:"task"`
		Operation string          `json:"operation"`
		Version   string          `json:"version"`
		Entries   []v2input.Entry `json:"entries"`
	}{task, s.Operation, "1", entries})
	if err != nil {
		return nil, err
	}
	node, err := v2input.NodeKey(s.Operation, 0)
	if err != nil {
		return nil, err
	}
	draft := v2input.DraftTask{ID: task, NodeKey: node, Operation: s.Operation, OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: publishedParams.ArtifactID, ParamsSHA: publishedParams.SHA256, InputDigest: digest, ExecutionProfile: plan.Profile}
	return canonical.Marshal(struct {
		SchemaVersion int               `json:"schema_version"`
		Task          v2input.DraftTask `json:"task"`
		Entries       []v2input.Entry   `json:"entries"`
		ReservedBytes int64             `json:"reserved_bytes"`
	}{1, draft, entries, s.ReservedBytes})
}
