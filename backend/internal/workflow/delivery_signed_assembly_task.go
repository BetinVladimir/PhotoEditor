package workflow

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/delivery"
	"photoeditor/backend/internal/v2input"
)

// FrozenSignedDeliveryAssemblyTask owns a distinct signed input closure while
// retaining the fixed v1 assemble task identity and CPU resource profile.
type FrozenSignedDeliveryAssemblyTask struct{ task FrozenDeliveryInitialTask }

func FreezeSignedDeliveryAssemblyTask(lease delivery.ExpansionLease, plan delivery.PreparePlan,
	envelope, params v2input.Ref, required, resolved []v2input.Entry,
	profile json.RawMessage) (FrozenSignedDeliveryAssemblyTask, error) {
	zero := FrozenSignedDeliveryAssemblyTask{}
	if !lease.Valid() || lease.Cursor != lease.SlotCount || lease.ParentTask != plan.TaskID() {
		return zero, v2input.ErrInvalid
	}
	workflow, err := v2input.WorkflowID(lease.Run)
	if err != nil || workflow != lease.Workflow { return zero, v2input.ErrInvalid }
	estimate, err := ParseResourceEstimate(profile)
	if err != nil || estimate.OutputBytes != 8388608 || estimate.VRAMBytes != 0 { return zero, v2input.ErrInvalid }
	var execution struct { Backend string `json:"backend"`; Renderer string `json:"renderer_version"` }
	if json.Unmarshal(profile, &execution) != nil || execution.Backend != "cpu" || execution.Renderer != "v2.2" {
		return zero, v2input.ErrInvalid
	}
	closure, err := v2input.CompileSignedDeliveryAssemblyClosure(workflow,
		v2input.DeliveryAssembleParams{SchemaVersion:1, Assembly:envelope}, params, required, resolved)
	if err != nil { return zero, err }
	id, err := v2input.TaskID(workflow, "v2.delivery.assemble", 0)
	if err != nil { return zero, err }
	draft := v2input.DraftTask{ID:id, NodeKey:"v2.delivery.assemble:0", Operation:"v2.delivery.assemble",
		OperationVersion:"1", ResourceClass:"cpu", ParamsArtifactID:params.ArtifactID,
		ParamsSHA:params.SHA256, InputDigest:closure.InputDigest,
		ExecutionProfile:append(json.RawMessage(nil),profile...)}
	return FrozenSignedDeliveryAssemblyTask{task:FrozenDeliveryInitialTask{draft:draft,closure:closure}},nil
}

func(f FrozenSignedDeliveryAssemblyTask)DraftTask()(v2input.DraftTask,error){return f.task.DraftTask()}
func(f FrozenSignedDeliveryAssemblyTask)PublishInputs(ctx context.Context,publisher v2input.Publisher)(v2input.GraphTask,error){
	return f.task.PublishInputs(ctx,publisher)
}
func(f FrozenSignedDeliveryAssemblyTask)CreateInTransaction(ctx context.Context,tx delivery.ProofTransaction,
	lease delivery.ExpansionLease,plan delivery.PreparePlan)error{
	draft,err:=f.DraftTask();if err!=nil{return err}
	return (delivery.ExpansionLeaseStore{DB:tx}).CreateBlockedAssemblyTask(ctx,lease,plan,draft,f.task.closure.ParamsJCS)
}
func(f FrozenSignedDeliveryAssemblyTask)RegisterInputsInTransaction(ctx context.Context,tx pgx.Tx,
	lease delivery.ExpansionLease,plan delivery.PreparePlan,graph v2input.GraphTask)error{
	if err:=ctx.Err();err!=nil{return err}
	if tx==nil{return v2input.ErrInvalid}
	draft,err:=f.DraftTask();if err!=nil||graph.ID!=draft.ID{return v2input.ErrInvalid}
	store:=delivery.ExpansionLeaseStore{DB:tx}
	ready,err:=store.SignedAssemblyTransportReady(ctx,lease,plan,draft,f.task.closure.ParamsJCS)
	if err!=nil||ready{return err}
	if err:=f.CreateInTransaction(ctx,tx,lease,plan);err!=nil{return err}
	if err:=v2input.RegisterInTransaction(ctx,tx,lease.Tenant,lease.Run,draft.ID,
		graph.Built,f.task.closure.Open,f.task.closure.Direct);err!=nil{return err}
	ready,err=store.SignedAssemblyTransportReady(ctx,lease,plan,draft,f.task.closure.ParamsJCS)
	if err!=nil{return err}
	if !ready{return v2input.ErrInvalid}
	return nil
}

// FreezePublishedSignedDeliveryAssemblyTask derives typed direct roots from
// the exact published document set. The caller resolves and reauthorizes all
// transitive WorkingImage/recipe inputs before supplying resolved.
func FreezePublishedSignedDeliveryAssemblyTask(lease delivery.ExpansionLease,plan delivery.PreparePlan,
	publication delivery.SignedAssemblyPublication,params v2input.Ref,actor uuid.UUID,
	slots []delivery.Slot,dependencies []delivery.SigningDependency,receipts []delivery.SignedDependencyReceipt,
	resolved []v2input.Entry,profile json.RawMessage)(FrozenSignedDeliveryAssemblyTask,error){
	required,err:=delivery.SignedAssemblyRequiredRoots(publication.AssemblyRef,
		publication.Documents.AssemblyJCS,publication.ShardJCS,publication.ClosureRef,
		publication.Documents.ClosureJCS,publication.EnvelopeRef,lease.Tenant,actor,lease.Run,
		slots,dependencies,receipts)
	if err!=nil{return FrozenSignedDeliveryAssemblyTask{},err}
	return FreezeSignedDeliveryAssemblyTask(lease,plan,publication.EnvelopeRef,params,required,resolved,profile)
}
