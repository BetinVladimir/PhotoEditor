package v2draft

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// ReflectionSubmissionService is installed for one tenant/project by a
// qualified operator. Clients cannot supply model, calibration, profile or
// quota policy; each durable stage repeats live SQL authorization.
type ReflectionSubmissionService struct {
	scheduler     *pgxpool.Pool
	catalog       *pgxpool.Pool
	objects       *objectstore.Store
	bucket        string
	tenant        uuid.UUID
	project       uuid.UUID
	pin           v2portrait.RestorationQualificationPin
	profile       []byte
	reservedBytes int64
}

func NewReflectionSubmissionService(scheduler, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string, tenant, project uuid.UUID,
	pin v2portrait.RestorationQualificationPin, baseProfile []byte,
	reservedBytes int64) (*ReflectionSubmissionService, error) {
	if scheduler == nil || catalog == nil || scheduler == catalog ||
		objects == nil || len(bucket) < 3 || len(bucket) > 63 ||
		tenant == uuid.Nil || project == uuid.Nil || pin.Operation != "image.reflection" ||
		reservedBytes < minRestorationDraftReservation ||
		reservedBytes > 9007199254740991 {
		return nil, v2input.ErrInvalid
	}
	if _, err := pin.Compile(); err != nil {
		return nil, err
	}
	normal, err := canonical.Bytes(baseProfile)
	if err != nil || !bytes.Equal(normal, baseProfile) ||
		len(baseProfile) < 2 || len(baseProfile) > 65536 {
		return nil, v2input.ErrInvalid
	}
	var profile map[string]json.RawMessage
	if json.Unmarshal(baseProfile, &profile) != nil || profile == nil ||
		profile["restoration_qualification"] != nil {
		return nil, v2input.ErrInvalid
	}
	probe, err := canonical.Marshal(map[string]any{"schema_version": 1,
		"model_lock_sha": pin.ModelLockSHA, "calibration": pin.Calibration})
	if err != nil {
		return nil, err
	}
	if _, err := v2portrait.CompilePinnedRestorationProfile("image.reflection",
		probe, baseProfile, pin); err != nil {
		return nil, err
	}
	return &ReflectionSubmissionService{scheduler: scheduler, catalog: catalog,
		objects: objects, bucket: bucket, tenant: tenant, project: project,
		pin: pin, profile: append([]byte(nil), baseProfile...),
		reservedBytes: reservedBytes}, nil
}

func (s *ReflectionSubmissionService) PrepareProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	request ReflectionPreparationRequest) (ReflectionClientIntent, error) {
	if s == nil {
		return ReflectionClientIntent{}, v2input.ErrInvalid
	}
	return (ReflectionPreparationService{Scheduler: s.scheduler, Tenant: s.tenant,
		Project: s.project, Pin: s.pin}).PrepareProjectKeyed(ctx, who, tenant,
		project, key, request)
}

func (s *ReflectionSubmissionService) SubmitProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	intent ReflectionClientIntent) (RestorationAdmissionTicket, error) {
	var zero RestorationAdmissionTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || tenant != s.tenant || project != s.project ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return zero, err
	}
	intent, err = DecodeReflectionClientIntent(raw)
	if err != nil {
		return zero, err
	}
	var selected struct {
		Base struct {
			AssetVersionID uuid.UUID `json:"asset_version_id"`
		} `json:"base"`
	}
	if json.Unmarshal(intent.Proposal, &selected) != nil ||
		selected.Base.AssetVersionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	submitter, err := NewSQLReflectionRestorationSubmitter(s.scheduler, s.catalog,
		s.objects, s.bucket, project, selected.Base.AssetVersionID)
	if err != nil {
		return zero, err
	}
	result, err := submitter.Submit(ctx, ReflectionDraftBeginRequest{
		Tenant: tenant, Project: project, Actor: who, Key: key, Intent: intent,
		Pin: s.pin, ReservedBytes: s.reservedBytes}, append([]byte(nil), s.profile...))
	if err != nil {
		return zero, err
	}
	if result.Ticket.RunID == uuid.Nil || result.Ticket.TaskID == uuid.Nil ||
		result.Ticket.State != "queued" {
		return zero, v2input.ErrInvalid
	}
	return result.Ticket, nil
}
