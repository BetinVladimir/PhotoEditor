package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
)

// CreateBlockedAssemblyTask reserves the fixed manifest cap exactly once.
// It neither installs input transport nor activates the task. SQL repeats live
// root/closure authority; caller may compose this in its admission transaction.
func (s ExpansionLeaseStore) CreateBlockedAssemblyTask(ctx context.Context, lease ExpansionLease, plan PreparePlan, task v2input.DraftTask, paramsRaw []byte) error {
	raw, err := s.assemblyTaskDescriptor(ctx, lease, plan, task, paramsRaw)
	if err != nil {
		return err
	}
	var accepted bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.create_v30_delivery_assembly_task($1,$2,$3::jsonb,$4,$5)`, lease.Expansion, lease.Token, string(raw), string(paramsRaw), int64(8388608)).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_ASSEMBLY_STALE", 409)
	}
	return nil
}

func (s ExpansionLeaseStore) assemblyTaskDescriptor(ctx context.Context, lease ExpansionLease, plan PreparePlan, task v2input.DraftTask, paramsRaw []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	owner, err := v2input.TaskID(lease.Workflow, "v2.delivery.assemble", 0)
	if err != nil || s.DB == nil || !lease.valid() || lease.Workflow != plan.workflow || lease.ParentTask != plan.task || lease.Cursor != lease.SlotCount || lease.SlotCount != plan.preparation.SlotCount || task.ID != owner || task.NodeKey != "v2.delivery.assemble:0" || task.Operation != "v2.delivery.assemble" || task.OperationVersion != "1" || task.ResourceClass != "cpu" || task.ParamsArtifactID == uuid.Nil || !shaPattern.MatchString(task.InputDigest) || len(paramsRaw) < 2 || len(paramsRaw) > 65536 || digest(paramsRaw) != task.ParamsSHA {
		return nil, v2input.ErrInvalid
	}
	var params v2input.DeliveryAssembleParams
	if json.Unmarshal(paramsRaw, &params) != nil || params.SchemaVersion != 1 || params.Assembly.ArtifactID == uuid.Nil || !shaPattern.MatchString(params.Assembly.SHA256) {
		return nil, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(params)
	if err != nil || !bytes.Equal(exact, paramsRaw) {
		return nil, v2input.ErrInvalid
	}
	var profile struct {
		Backend  string `json:"backend"`
		Renderer string `json:"renderer_version"`
		Estimate struct {
			VRAM   int64 `json:"vram_bytes"`
			Output int64 `json:"output_bytes"`
		} `json:"resource_estimate"`
	}
	if json.Unmarshal(task.ExecutionProfile, &profile) != nil || profile.Backend != "cpu" || profile.Renderer != "v2.2" || profile.Estimate.VRAM != 0 || profile.Estimate.Output != 8388608 {
		return nil, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(task)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// AssemblyTransportReady acknowledges an exact previously registered closure.
// False means transport must be registered/repaired; errors must abort admission.
func (s ExpansionLeaseStore) AssemblyTransportReady(ctx context.Context, lease ExpansionLease, plan PreparePlan, task v2input.DraftTask, paramsRaw []byte) (bool, error) {
	raw, err := s.assemblyTaskDescriptor(ctx, lease, plan, task, paramsRaw)
	if err != nil {
		return false, err
	}
	var ready bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.v30_delivery_assembly_transport_ready($1,$2,$3::jsonb,$4)`, lease.Expansion, lease.Token, string(raw), string(paramsRaw)).Scan(&ready); err != nil {
		return false, err
	}
	return ready, nil
}

// ActivateAssembly closes the parent expansion and makes the registered task
// runnable atomically. Use a transaction-backed store; roll back on any error.
// This transition is not an acknowledgement API for an already activated task.
func (s ExpansionLeaseStore) ActivateAssembly(ctx context.Context, lease ExpansionLease, plan PreparePlan, task v2input.DraftTask, paramsRaw []byte) error {
	raw, err := s.assemblyTaskDescriptor(ctx, lease, plan, task, paramsRaw)
	if err != nil {
		return err
	}
	var accepted bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.activate_v30_delivery_assembly($1,$2,$3::jsonb,$4)`, lease.Expansion, lease.Token, string(raw), string(paramsRaw)).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_ASSEMBLY_STALE", 409)
	}
	return nil
}

// ActivateSignedAssembly is the fenced SQL transition after the scheduler's
// independent current V31 object verification. It never accepts the ordinary
// off-mode transport/source predicate.
func (s ExpansionLeaseStore) ActivateSignedAssembly(ctx context.Context, lease ExpansionLease, plan PreparePlan, task v2input.DraftTask, paramsRaw []byte) error {
	raw, err := s.assemblyTaskDescriptor(ctx, lease, plan, task, paramsRaw)
	if err != nil {
		return err
	}
	var accepted bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.activate_v30_delivery_signed_assembly($1,$2,$3::jsonb,$4)`, lease.Expansion, lease.Token, string(raw), string(paramsRaw)).Scan(&accepted); err != nil {
		return err
	}
	if !accepted {
		return domain.NewError("DELIVERY_SIGNED_ASSEMBLY_STALE", 409)
	}
	return nil
}

// AssemblyActivated acknowledges the exact committed transition even after its
// lease expired or the task terminated. It does not authorize new execution.
func (s ExpansionLeaseStore) AssemblyActivated(ctx context.Context, lease ExpansionLease, plan PreparePlan, task v2input.DraftTask, paramsRaw []byte) (bool, error) {
	raw, err := s.assemblyTaskDescriptor(ctx, lease, plan, task, paramsRaw)
	if err != nil {
		return false, err
	}
	var accepted bool
	if err := s.DB.QueryRow(ctx, `SELECT photo.v30_delivery_assembly_activated($1,$2,$3::jsonb,$4)`, lease.Expansion, lease.Token, string(raw), string(paramsRaw)).Scan(&accepted); err != nil {
		return false, err
	}
	return accepted, nil
}
