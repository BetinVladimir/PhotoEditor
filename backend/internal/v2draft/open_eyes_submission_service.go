package v2draft

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
	"photoeditor/backend/internal/v2qualification"
)

type OpenEyesSubmissionService struct {
	Preparation *OpenEyesPreparationService
	Scheduler   *pgxpool.Pool
	Catalog     *pgxpool.Pool
	Objects     *objectstore.Store
	Bucket      string
	Tenant      uuid.UUID
	Project     uuid.UUID
	Pin         v2portrait.DonorQualificationPin
	Locks       v2qualification.DeploymentLocks
	PatchWidth  int64
	PatchHeight int64
	Capacity    v2portrait.DonorResourceEstimate
	Reserved    int64
}

func NewOpenEyesSubmissionService(scheduler, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string, tenant, project uuid.UUID,
	pin v2portrait.DonorQualificationPin, locks v2qualification.DeploymentLocks,
	patchWidth, patchHeight int64, capacity v2portrait.DonorResourceEstimate,
	reserved int64) (*OpenEyesSubmissionService, error) {
	if scheduler == nil || catalog == nil || scheduler == catalog || objects == nil ||
		len(bucket) < 3 || len(bucket) > 63 || tenant == uuid.Nil || project == uuid.Nil ||
		reserved < minRestorationDraftReservation || reserved > 9007199254740991 ||
		capacity.OutputBytes < 1 || capacity.OutputBytes > reserved-2162688 {
		return nil, v2input.ErrInvalid
	}
	preparation, err := NewOpenEyesPreparationService(scheduler, tenant, project, pin)
	if err != nil {
		return nil, err
	}
	policySHA, err := v2portrait.DonorPolicySHA(pin.PolicyID, locks)
	if err != nil || policySHA != pin.PolicySHA || pin.RuntimeLockSHA != locks.RuntimeLockSHA ||
		pin.ModelLockSHA != locks.ModelLockSHA || pin.DeviceProfileSHA != locks.DeviceProfileSHA ||
		pin.DatasetSHA != locks.DatasetSHA || pin.IssuerID.String() != locks.IssuerID {
		return nil, v2input.ErrInvalid
	}
	if patchWidth < 1 || patchHeight < 1 || patchWidth > 1048576 ||
		patchHeight > 1048576/patchWidth {
		return nil, v2input.ErrInvalid
	}
	return &OpenEyesSubmissionService{Preparation: preparation, Scheduler: scheduler,
		Catalog: catalog, Objects: objects, Bucket: bucket, Tenant: tenant,
		Project: project, Pin: pin, Locks: locks, PatchWidth: patchWidth,
		PatchHeight: patchHeight, Capacity: capacity, Reserved: reserved}, nil
}

func (s *OpenEyesSubmissionService) PrepareProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	selection OpenEyesClientSelection) (OpenEyesClientPreparation, error) {
	if s == nil || s.Preparation == nil {
		return OpenEyesClientPreparation{}, v2input.ErrInvalid
	}
	return s.Preparation.PrepareProjectKeyed(ctx, who, tenant, project, key, selection)
}

// SubmitProjectKeyed repeats the live preparation, then enters the durable
// donor path. No queued ticket is returned until the registered graph commits.
func (s *OpenEyesSubmissionService) SubmitProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	intent OpenEyesClientIntent) (DonorQueuedTicket, error) {
	var zero DonorQueuedTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || s.Preparation == nil || s.Scheduler == nil || s.Catalog == nil ||
		s.Objects == nil || tenant != s.Tenant || project != s.Project ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return zero, err
	}
	intent, err = DecodeOpenEyesClientIntent(raw)
	if err != nil {
		return zero, err
	}
	if ticket, replayErr := (SQLOpenEyesQueuedReplay{DB: s.Scheduler}).Read(ctx,
		who, tenant, key, intent); replayErr == nil {
		return ticket, nil
	} else if !errors.Is(replayErr, v2input.ErrExpired) {
		return zero, replayErr
	}
	issue := OpenEyesDraftIssuer{Preparation: s.Preparation,
		Beginner: SQLOpenEyesDraftBeginner{DB: s.Scheduler},
		Issuer: v2portrait.OpenEyesConfirmationService{DB: s.Scheduler,
			Objects: s.Objects, Bucket: s.Bucket},
		Pin: s.Pin, Reserved: s.Reserved}
	issued, err := issue.IssueAfterBegin(ctx, who, tenant, project, key, intent)
	if err != nil {
		return zero, err
	}
	deps, err := NewSQLOpenEyesRecoveryDependencies(s.Scheduler, s.Catalog,
		s.Objects, s.Bucket, project, issued.Params.Base.AssetVersionID,
		issued.Params.Donor.AssetVersionID, s.Locks, s.PatchWidth, s.PatchHeight,
		s.Capacity)
	if err != nil {
		return zero, err
	}
	manual, err := deps.LoadManualProducer(ctx, who, tenant, intent.Snapshot,
		intent, issued.Params)
	if err != nil {
		return zero, err
	}
	if err = deps.PrimeParamsIntent(ctx, who, tenant, project,
		issued.Identity.Run, issued.Identity.Workflow, intent.Ordinal, issued.Params,
		s.Pin, manual); err != nil {
		return zero, err
	}
	ticket, err := deps.Params.RecoverPublishAndAdmit(ctx, who, tenant, project,
		intent.Shoot, issued.Identity.Run, issued.Identity.Workflow, deps.Loader,
		manual, deps.Documents)
	if err != nil {
		return zero, err
	}
	if ticket.RunID != issued.Identity.Run || ticket.WorkflowID != issued.Identity.Workflow ||
		ticket.TaskID != issued.Identity.Owner || ticket.State != "queued" {
		return zero, v2input.ErrInvalid
	}
	return ticket, nil
}
