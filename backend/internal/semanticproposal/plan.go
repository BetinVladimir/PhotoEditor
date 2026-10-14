package semanticproposal

import (
	"bytes"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

// WorkflowPlan owns the validated admission payload. It cannot activate tasks;
// final SQL must repeat authorization, object pins, qualification and quotas.
type protectionProof struct {
	Binding     v2input.SessionVersionBinding
	Root, Child v2input.Ref
}

type WorkflowPlan struct {
	nativeTargetsRequired            bool
	nativeReferenceRequired          bool
	nativeReference                  *nativeSourceContinuationProof
	sourcesRequired                  bool
	sources                          []sourcePreparationProof
	transferQualificationRequired    bool
	transferQualification            *transferQualificationPreparationProof
	person                           []personProtectionProof
	protection                       []protectionProof
	snapshotRef                      v2input.Ref
	snapshotBody                     json.RawMessage
	inventory                        CatalogInventory
	tenant, actor                    uuid.UUID
	policySHA                        string
	run, workflow, project, snapshot uuid.UUID
	requestSHA, configSHA            string
	intent                           v2input.Ref
	config                           json.RawMessage
	tasks                            []v2input.DraftTask
	frozen                           []v2input.FrozenSemanticTask
}

func (p WorkflowPlan) Inventory() CatalogInventory {
	return CatalogInventory{Scope: p.inventory.Scope, Targets: append([]v2input.SessionVersionBinding(nil), p.inventory.Targets...)}
}
func (p WorkflowPlan) ConfigJSON() json.RawMessage { return append(json.RawMessage(nil), p.config...) }
func (p WorkflowPlan) DraftTasks() []v2input.DraftTask {
	result := append([]v2input.DraftTask(nil), p.tasks...)
	for i := range result {
		result[i].ExecutionProfile = append(json.RawMessage(nil), result[i].ExecutionProfile...)
	}
	return result
}
func (p WorkflowPlan) RunID() uuid.UUID       { return p.run }
func (p WorkflowPlan) WorkflowID() uuid.UUID  { return p.workflow }
func (p WorkflowPlan) ProjectID() uuid.UUID   { return p.project }
func (p WorkflowPlan) SnapshotID() uuid.UUID  { return p.snapshot }
func (p WorkflowPlan) RequestSHA() string     { return p.requestSHA }
func (p WorkflowPlan) ConfigSHA() string      { return p.configSHA }
func (p WorkflowPlan) IntentRef() v2input.Ref { return p.intent }
func CompileWorkflowPlan(request Request, session uuid.UUID, published PublishedIntent, snapshotRef v2input.Ref, snapshotBody json.RawMessage, tasks []v2input.FrozenSemanticTask, qualification v2input.SemanticQualificationPin) (WorkflowPlan, error) {
	var empty WorkflowPlan
	if request.TenantID == uuid.Nil || request.ActorID == uuid.Nil || request.RunID == uuid.Nil || session == uuid.Nil || published.Inventory.Scope.ProjectID == uuid.Nil {
		return empty, v2input.ErrInvalid
	}
	body, err := canonical.Marshal(request.Intent)
	if err != nil || !bytes.Equal(body, request.Body) {
		return empty, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(request.RunID)
	if err != nil || workflow != request.WorkflowID {
		return empty, v2input.ErrInvalid
	}
	sha, err := canonical.Hash(struct {
		Tenant uuid.UUID                  `json:"tenant_id"`
		Actor  uuid.UUID                  `json:"actor_id"`
		Intent v2input.SemanticEditIntent `json:"intent"`
	}{request.TenantID, request.ActorID, request.Intent})
	if err != nil || sha != request.RequestSHA {
		return empty, v2input.ErrInvalid
	}
	targets, err := BindTargets(request, snapshotRef, snapshotBody)
	if err != nil {
		return empty, err
	}
	inventory := published.Inventory
	if len(targets) != inventory.Scope.ItemCount || len(inventory.Targets) != len(targets) {
		return empty, v2input.ErrInvalid
	}
	for i := range targets {
		if targets[i] != inventory.Targets[i] {
			return empty, v2input.ErrConflict
		}
	}
	_, config, configSHA, err := v2input.CompileSemanticProposalConfig(inventory.Scope.ProjectID, session, request.RunID, published.Ref, request.Body, snapshotRef, snapshotBody, tasks, qualification)
	if err != nil {
		return empty, err
	}
	drafts := make([]v2input.DraftTask, len(tasks))
	frozen := make([]v2input.FrozenSemanticTask, len(tasks))
	for _, task := range tasks {
		pin, err := task.ProtectionPin()
		if err != nil {
			return empty, err
		}
		draft, err := task.DraftTask()
		if err != nil {
			return empty, err
		}
		drafts[pin.Ordinal] = draft
		frozen[pin.Ordinal] = task
	}
	return WorkflowPlan{snapshotRef: snapshotRef, snapshotBody: append(json.RawMessage(nil), snapshotBody...), inventory: CatalogInventory{Scope: inventory.Scope, Targets: append([]v2input.SessionVersionBinding(nil), inventory.Targets...)}, tenant: request.TenantID, actor: request.ActorID, policySHA: qualification.PolicySHA, run: request.RunID, workflow: request.WorkflowID, project: inventory.Scope.ProjectID, snapshot: request.Intent.TargetSnapshotID, requestSHA: request.RequestSHA, configSHA: configSHA, intent: published.Ref, config: config, tasks: drafts, frozen: frozen}, nil
}

// DraftShell requires an explicit server-owned reservation covering task
// outputs plus future assembly/transport overhead. It creates no SQL state.
func (p WorkflowPlan) DraftShell(reservedBytes int64) (v2input.DraftShell, error) {
	const maximum int64 = 9007199254740991
	if p.tenant == uuid.Nil || p.actor == uuid.Nil || p.run == uuid.Nil || len(p.tasks) == 0 || reservedBytes <= 0 || reservedBytes > maximum {
		return v2input.DraftShell{}, v2input.ErrInvalid
	}
	var minimum int64
	for _, task := range p.tasks {
		var profile struct {
			Resources struct {
				Output *int64 `json:"output_bytes"`
			} `json:"resource_estimate"`
		}
		if json.Unmarshal(task.ExecutionProfile, &profile) != nil || profile.Resources.Output == nil || *profile.Resources.Output <= 0 || *profile.Resources.Output > maximum-minimum {
			return v2input.DraftShell{}, v2input.ErrInvalid
		}
		minimum += *profile.Resources.Output
	}
	if reservedBytes < minimum {
		return v2input.DraftShell{}, v2input.ErrInvalid
	}
	return v2input.DraftShell{Tenant: p.tenant, Run: p.run, WorkflowID: p.workflow, Project: p.project, Snapshot: p.snapshot, Actor: p.actor, Workflow: "semantic", PolicySHA: p.policySHA, ConfigSHA: p.configSHA, Config: p.ConfigJSON(), Tasks: p.DraftTasks(), Edges: []v2input.DraftEdge{}, Bytes: reservedBytes}, nil
}
