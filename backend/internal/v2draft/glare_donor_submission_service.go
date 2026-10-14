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

const MaxGlareDonorIntentBytes = 65536 + 4096

type GlareDonorClientIntent struct {
	SchemaVersion     int             `json:"schema_version"`
	Shoot             uuid.UUID       `json:"shoot_session_id"`
	Snapshot          uuid.UUID       `json:"snapshot_id"`
	Ordinal           int             `json:"ordinal"`
	Proposal          json.RawMessage `json:"params_proposal"`
	SourceObservation uuid.UUID       `json:"source_observation_id"`
	DonorObservation  uuid.UUID       `json:"donor_observation_id"`
}

func DecodeGlareDonorClientIntent(raw []byte) (GlareDonorClientIntent, error) {
	var zero GlareDonorClientIntent
	if len(raw) < 2 || len(raw) > MaxGlareDonorIntentBytes {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normal, raw) {
		return zero, v2input.ErrInvalid
	}
	var intent GlareDonorClientIntent
	if json.Unmarshal(raw, &intent) != nil || intent.SchemaVersion != 1 ||
		intent.Shoot == uuid.Nil || intent.Snapshot == uuid.Nil ||
		intent.Ordinal < 0 || intent.Ordinal >= 5000 ||
		intent.SourceObservation == uuid.Nil || intent.DonorObservation == uuid.Nil ||
		intent.SourceObservation == intent.DonorObservation ||
		len(intent.Proposal) < 2 || len(intent.Proposal) > 65536 {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(intent)
	if err != nil || !bytes.Equal(exact, raw) {
		return zero, v2input.ErrInvalid
	}
	return intent, nil
}

// GlareDonorSubmissionService is an operator-installed, single-project API
// capability. Its qualification, profile and reservation are never read from
// client JSON; Begin and Admit still repeat current SQL/object authority.
type GlareDonorSubmissionService struct {
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

func NewGlareDonorSubmissionService(scheduler, catalog *pgxpool.Pool,
	objects *objectstore.Store, bucket string, tenant, project uuid.UUID,
	pin v2portrait.RestorationQualificationPin, baseProfile []byte,
	reservedBytes int64) (*GlareDonorSubmissionService, error) {
	if scheduler == nil || catalog == nil || scheduler == catalog || objects == nil ||
		len(bucket) < 3 || len(bucket) > 63 || tenant == uuid.Nil || project == uuid.Nil ||
		pin.Operation != "portrait.glare" || reservedBytes < minRestorationDraftReservation ||
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
	probe, err := canonical.Marshal(map[string]any{
		"schema_version": 1, "model_lock_sha": pin.ModelLockSHA,
		"calibration": pin.Calibration,
	})
	if err != nil {
		return nil, err
	}
	if _, err := v2portrait.CompilePinnedRestorationProfile("portrait.glare",
		probe, baseProfile, pin); err != nil {
		return nil, err
	}
	return &GlareDonorSubmissionService{scheduler: scheduler, catalog: catalog,
		objects: objects, bucket: bucket, tenant: tenant, project: project, pin: pin,
		profile: append([]byte(nil), baseProfile...), reservedBytes: reservedBytes}, nil
}

func (s *GlareDonorSubmissionService) SubmitProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	intent GlareDonorClientIntent) (RestorationAdmissionTicket, error) {
	var zero RestorationAdmissionTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || tenant != s.tenant || project != s.project ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if intent.SchemaVersion != 1 || intent.Shoot == uuid.Nil || intent.Snapshot == uuid.Nil ||
		intent.Ordinal < 0 || intent.Ordinal >= 5000 ||
		intent.SourceObservation == uuid.Nil || intent.DonorObservation == uuid.Nil ||
		intent.SourceObservation == intent.DonorObservation {
		return zero, v2input.ErrInvalid
	}
	var params struct {
		Base struct {
			AssetVersionID uuid.UUID `json:"asset_version_id"`
		} `json:"base"`
		Donor struct {
			AssetVersionID uuid.UUID `json:"asset_version_id"`
		} `json:"donor"`
	}
	if json.Unmarshal(intent.Proposal, &params) != nil ||
		params.Base.AssetVersionID == uuid.Nil || params.Donor.AssetVersionID == uuid.Nil ||
		params.Base.AssetVersionID == params.Donor.AssetVersionID {
		return zero, v2input.ErrInvalid
	}
	submitter, err := NewSQLGlareDonorRestorationSubmitter(s.scheduler, s.catalog,
		s.objects, s.bucket, project, params.Base.AssetVersionID,
		params.Donor.AssetVersionID)
	if err != nil {
		return zero, err
	}
	result, err := submitter.Submit(ctx, GlareDonorDraftBeginRequest{
		Tenant: tenant, Project: project, Shoot: intent.Shoot, Snapshot: intent.Snapshot,
		Actor: who, Key: key, Ordinal: intent.Ordinal, Proposal: append([]byte(nil), intent.Proposal...),
		Pin: s.pin, SourceObservation: intent.SourceObservation,
		DonorObservation: intent.DonorObservation, ReservedBytes: s.reservedBytes},
		append([]byte(nil), s.profile...))
	if err != nil {
		return zero, err
	}
	if result.Ticket.RunID == uuid.Nil || result.Ticket.TaskID != result.TaskID ||
		result.Ticket.State != "queued" {
		return zero, v2input.ErrInvalid
	}
	return result.Ticket, nil
}
