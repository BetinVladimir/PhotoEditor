package v2region

import (
	"context"
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
	Preparation Service
	DB          *pgxpool.Pool
	Profiles    RegionProfileProvider
	Resolver    RegionInputResolver
	Params      RegionParamsPublisher
	Closure     v2input.Publisher
}

// Queue creates an authorized immutable preview job. Runtime claims and quota
// estimates come solely from Profiles, never the public request. No renderer
// qualification or catalog activation is implied by installing this service.
func (s QueueService) Queue(ctx context.Context, who auth.Identity, tenant, asset uuid.UUID, key string, request RegionPreviewRequest) (v2input.JobTicket, error) {
	var empty v2input.JobTicket
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	if s.DB == nil || s.Profiles == nil || s.Resolver == nil || s.Params == nil || s.Closure == nil || who.SessionID == uuid.Nil || who.UserID == uuid.Nil || tenant == uuid.Nil || asset == uuid.Nil {
		return empty, ErrRegionInvalid
	}
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
	shape, err := prepared.Plan.SourceShape()
	if err != nil {
		return empty, err
	}
	policy, err := s.Profiles.CompileRegionProfile(ctx, prepared.Plan)
	if err != nil {
		return empty, err
	}
	if !validRegionSHA(policy.PolicySHA) || policy.ReservedBytes < 1 || policy.ReservedBytes > 9007199254740991 {
		return empty, ErrRegionInvalid
	}
	if err := validateRegionSourceBudget(policy.Profile, shape); err != nil {
		return empty, err
	}
	if err := validateRegionArtifactBudget(policy.Profile); err != nil {
		return empty, err
	}
	node, err := prepared.Plan.PrepareNode(ctx, who, task, s.Resolver, s.Params, policy.Profile)
	if err != nil {
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
	requestSHA, err := canonical.Hash(struct {
		Tenant  uuid.UUID            `json:"tenant_id"`
		Asset   uuid.UUID            `json:"asset_id"`
		Request RegionPreviewRequest `json:"request"`
	}{tenant, asset, request})
	if err != nil {
		return empty, err
	}
	return node.Admit(ctx, s.DB, s.Closure, who, key, snapshot.ID, policy.PolicySHA, policy.ReservedBytes, requestSHA)
}
