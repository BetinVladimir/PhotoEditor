package v2region

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/v2input"
)

var ErrRegionUnavailable = errors.New("REGION_PREVIEW_UNAVAILABLE")

type RegionExecutionPolicy struct {
	Profile       []byte
	PolicySHA     string
	ReservedBytes int64
}

// RegionProfileProvider is server-owned. It must qualify installed runtime and
// budget the complete recipe, not just source mapping, before returning policy.
// SourceShape provides verified full-canvas dimensions/alpha; the provider must
// resolve qualified style/model operator costs in addition to those inputs.
type RegionProfileProvider interface {
	CompileRegionProfile(context.Context, FrozenRegionPlan) (RegionExecutionPolicy, error)
}

type QueueService struct {
	Preparation  RegionPreparation
	DB           *pgxpool.Pool
	Scheduler    *pgxpool.Pool
	Profiles     RegionProfileProvider
	Resolver     RegionInputResolver
	Params       RegionParamsPublisher
	Closure      v2input.Publisher
	Dependencies RegionDependencyFactory
}

// Queue creates an authorized immutable preview job. Runtime claims and quota
// estimates come solely from Profiles, never the public request. No renderer
// qualification or catalog activation is implied by installing this service.
func (s QueueService) Queue(ctx context.Context, who auth.Identity, tenant, asset uuid.UUID, key string, request RegionPreviewRequest) (v2input.JobTicket, error) {
	var empty v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if who.SessionID == uuid.Nil || who.UserID == uuid.Nil || tenant == uuid.Nil || asset == uuid.Nil {
		return empty, ErrRegionInvalid
	}
	if s.DB == nil { return empty, ErrRegionUnavailable }
	run, err := v2input.RunID(tenant, who.UserID, key)
	if err != nil {
		return empty, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil {
		return empty, err
	}
	task, err := v2input.TaskID(workflow, "region.preview", 0)
	if err != nil {
		return empty, err
	}


 requestSHA,err:=regionRequestSHA(tenant,asset,request);if err!=nil{return empty,err}
 if ticket,found,e:=s.acceptedRegion(ctx,who,tenant,asset,run,workflow,task,key,requestSHA,request);e!=nil||found{return ticket,e}
 if s.Preparation == nil || s.Scheduler == nil || s.Profiles == nil || (s.Dependencies == nil && (s.Resolver == nil || s.Params == nil || s.Closure == nil)) { return empty, ErrRegionUnavailable }

	// Avoid object downloads/publication and frozen orphan snapshots while disabled.
	// This early check is advisory: Begin and Seal recheck live catalog authority.
	err = database.TenantTransaction(ctx, s.DB, tenant, func(tx pgx.Tx) error {
		var available bool
		if err := tx.QueryRow(ctx, "SELECT photo.v26_region_available($1)", tenant).Scan(&available); err != nil {
			return err
		}
		if !available {
			return ErrRegionUnavailable
		}
		return nil
	})
	if err != nil {
		return empty, err
	}
	prepared, err := s.Preparation.Prepare(ctx, tenant, asset, who.UserID, who.SessionID, request)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if err := prepared.Plan.validateQueueScope(tenant, asset, request); err != nil {
		return empty, err
	}
	_, err = prepared.Plan.SourceShape()
	if err != nil {
		return empty, err
	}
	policy, err := s.Profiles.CompileRegionProfile(ctx, prepared.Plan)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	policy, err = validateQueuedRegionPolicy(prepared.Plan, policy)
	if err != nil {
		return empty, err
	}

	dependencies := RegionQueueDependencies{Resolver: s.Resolver, Params: s.Params, Closure: s.Closure}
	if s.Dependencies != nil {
		dependencies, err = s.Dependencies.ForRegionPlan(ctx, prepared.Plan, run)
		if err != nil {
			return empty, err
		}
	}
	if dependencies.Resolver == nil || dependencies.Params == nil || dependencies.Closure == nil {
		return empty, ErrRegionUnavailable
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	node, err := prepared.Plan.PrepareNode(ctx, who, task, dependencies.Resolver, dependencies.Params, policy.Profile)
	if err != nil {
		return empty, err
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	var snapshot RegionSourceSnapshot
	err = database.TenantTransaction(ctx, s.DB, tenant, func(tx pgx.Tx) error {
		var e error
		snapshot, e = prepared.Plan.FreezeSnapshotInTransaction(ctx, tx, who, key)
		return e
	})
	if err != nil {
		return empty, err
	}
	return node.Admit(ctx, s.Scheduler, dependencies.Closure, who, key, snapshot.ID, policy.PolicySHA, policy.ReservedBytes, requestSHA)
}

// Bound and detach provider-owned bytes before budget parsing or dependencies.
func ownRegionExecutionPolicy(policy RegionExecutionPolicy) (RegionExecutionPolicy, error) {
	if !validRegionSHA(policy.PolicySHA) || policy.ReservedBytes < 1 || policy.ReservedBytes > 9007199254740991 || len(policy.Profile) < 1 || len(policy.Profile) > 64*1024 {
		return RegionExecutionPolicy{}, ErrRegionInvalid
	}
	policy.Profile = append([]byte(nil), policy.Profile...)
	return policy, nil
}

// All newly queued Region tasks require complete measured qualification. Keep
// the bound quota/policy synchronized with the exact compiler-owned projection
// before resolving inputs, publishing Params or freezing an orphan snapshot.
func validateQueuedRegionPolicy(plan FrozenRegionPlan, policy RegionExecutionPolicy) (RegionExecutionPolicy, error) {
	invalid := func() (RegionExecutionPolicy, error) { return RegionExecutionPolicy{}, ErrRegionInvalid }
	owned, err := ownRegionExecutionPolicy(policy)
	if err != nil {
		return invalid()
	}
	owned.Profile, err = canonical.Bytes(owned.Profile)
	if err != nil {
		return invalid()
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(owned.Profile, &fields) != nil || fields == nil {
		return invalid()
	}
	raw, present := fields["region_qualification"]
	if !present {
		return RegionExecutionPolicy{}, ErrRegionUnavailable
	}
	if err = plan.validatePinnedProfile(fields); err != nil {
		return invalid()
	}
	var pin RegionCalibrationPin
	var estimate RegionResourceEstimate
	if json.Unmarshal(raw, &pin) != nil || json.Unmarshal(fields["resource_estimate"], &estimate) != nil ||
		owned.PolicySHA != pin.Qualification.PolicySHA || owned.ReservedBytes != estimate.OutputBytes {
		return invalid()
	}
	shape, err := plan.SourceShape()
	if err != nil {
		return invalid()
	}
	if validateRegionSourceBudget(owned.Profile, shape) != nil || validateRegionArtifactBudget(owned.Profile) != nil {
		return invalid()
	}
	return owned, nil
}

// Bind preparation output to the public call before qualification or publication.
// SQL still independently rechecks actor/session and live access at admission.
func (p FrozenRegionPlan) validateQueueScope(tenant, asset uuid.UUID, request RegionPreviewRequest) error {
	document, err := p.decode()
	if err != nil || tenant == uuid.Nil || asset == uuid.Nil || document.TenantID != tenant || document.AssetID != asset || document.ProjectID == uuid.Nil {
		return ErrRegionInvalid
	}
	expected, err := canonical.Marshal(request)
	if err != nil {
		return ErrRegionInvalid
	}
	actual, err := canonical.Marshal(document.Request)
	if err != nil || !bytes.Equal(actual, expected) {
		return ErrRegionInvalid
	}
	return nil
}
