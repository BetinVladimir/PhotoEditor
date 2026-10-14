package workflow

import (
	"context"
	"encoding/json"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// FrozenDeliveryAssemblyTask owns a descriptor and one immutable inventory for
// shell creation, publication, registration and recovery. Required roots and the
// resolved inventory must come from current verified root/shard evidence. This
// constructor does not establish SQL source authority or infer missing roots.
type FrozenDeliveryAssemblyTask struct{ task FrozenDeliveryInitialTask }

func FreezeDeliveryAssemblyTask(lease delivery.ExpansionLease, plan delivery.PreparePlan, assembly, params v2input.Ref, required, resolved []v2input.Entry, profile json.RawMessage) (FrozenDeliveryAssemblyTask, error) {
	zero := FrozenDeliveryAssemblyTask{}
	if lease.ParentTask != plan.TaskID() || lease.Cursor != lease.SlotCount || lease.SlotCount < 1 || lease.SlotCount > 80000 {
		return zero, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(lease.Run)
	if err != nil || workflow != lease.Workflow {
		return zero, v2input.ErrInvalid
	}
	estimate, err := ParseResourceEstimate(profile)
	if err != nil || estimate.OutputBytes != 8388608 || estimate.VRAMBytes != 0 {
		return zero, v2input.ErrInvalid
	}
	var execution struct {
		Backend  string `json:"backend"`
		Renderer string `json:"renderer_version"`
	}
	if json.Unmarshal(profile, &execution) != nil || execution.Backend != "cpu" || execution.Renderer != "v2.2" {
		return zero, v2input.ErrInvalid
	}
	closure, err := v2input.CompileDeliveryAssemblyClosure(workflow, v2input.DeliveryAssembleParams{SchemaVersion: 1, Assembly: assembly}, params, required, resolved)
	if err != nil {
		return zero, err
	}
	id, err := v2input.TaskID(workflow, "v2.delivery.assemble", 0)
	if err != nil {
		return zero, err
	}
	draft := v2input.DraftTask{ID: id, NodeKey: "v2.delivery.assemble:0", Operation: "v2.delivery.assemble", OperationVersion: "1", ResourceClass: "cpu", ParamsArtifactID: params.ArtifactID, ParamsSHA: params.SHA256, InputDigest: closure.InputDigest, ExecutionProfile: append(json.RawMessage(nil), profile...)}
	return FrozenDeliveryAssemblyTask{task: FrozenDeliveryInitialTask{draft: draft, closure: closure}}, nil
}
func (f FrozenDeliveryAssemblyTask) DraftTask() (v2input.DraftTask, error) { return f.task.DraftTask() }
func (f FrozenDeliveryAssemblyTask) PublishInputs(ctx context.Context, publisher v2input.Publisher) (v2input.GraphTask, error) {
	return f.task.PublishInputs(ctx, publisher)
}
func (f FrozenDeliveryAssemblyTask) CreateInTransaction(ctx context.Context, tx delivery.ProofTransaction, lease delivery.ExpansionLease, plan delivery.PreparePlan) error {
	draft, err := f.DraftTask()
	if err != nil {
		return err
	}
	return (delivery.ExpansionLeaseStore{DB: tx}).CreateBlockedAssemblyTask(ctx, lease, plan, draft, f.task.closure.ParamsJCS)
}

// RegisterInputsInTransaction retries a sealed transport using its dedicated
// receipt. Any failure requires caller rollback; no object IO occurs here.
func (f FrozenDeliveryAssemblyTask) RegisterInputsInTransaction(ctx context.Context, tx pgx.Tx, lease delivery.ExpansionLease, plan delivery.PreparePlan, graph v2input.GraphTask) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	draft, err := f.DraftTask()
	if err != nil || graph.ID != draft.ID {
		return v2input.ErrInvalid
	}
	store := delivery.ExpansionLeaseStore{DB: tx}
	ready, err := store.AssemblyTransportReady(ctx, lease, plan, draft, f.task.closure.ParamsJCS)
	if err != nil || ready {
		return err
	}
	if err := f.CreateInTransaction(ctx, tx, lease, plan); err != nil {
		return err
	}
	if err := v2input.RegisterInTransaction(ctx, tx, lease.Tenant, lease.Run, draft.ID, graph.Built, f.task.closure.Open, f.task.closure.Direct); err != nil {
		return err
	}
	ready, err = store.AssemblyTransportReady(ctx, lease, plan, draft, f.task.closure.ParamsJCS)
	if err != nil {
		return err
	}
	if !ready {
		return v2input.ErrInvalid
	}
	return nil
}

// FreezePublishedDeliveryAssemblyTask derives required roots from exact documents
// before pinning the independently resolved transitive inventory.
func FreezePublishedDeliveryAssemblyTask(ctx context.Context, lease delivery.ExpansionLease, plan delivery.PreparePlan, assembly, params v2input.Ref, rootRaw []byte, shards [][]byte, resolved []v2input.Entry, profile json.RawMessage) (FrozenDeliveryAssemblyTask, error) {
	required, err := plan.AssemblyRequiredInputs(ctx, lease.Run, assembly, rootRaw, shards)
	if err != nil {
		return FrozenDeliveryAssemblyTask{}, err
	}
	return FreezeDeliveryAssemblyTask(lease, plan, assembly, params, required, resolved, profile)
}
