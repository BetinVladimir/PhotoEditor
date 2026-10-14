package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type restorationParamsIntent struct {
	Actor         uuid.UUID                              `json:"actor_id"`
	Session       uuid.UUID                              `json:"auth_session_id"`
	NodeKey       string                                 `json:"node_key"`
	Origin        RestorationManualOrigin                `json:"origin"`
	Owner         uuid.UUID                              `json:"owner_id"`
	Params        string                                 `json:"params_jcs"`
	ParamsSHA     string                                 `json:"params_sha"`
	Qualification v2portrait.RestorationQualificationPin `json:"qualification"`
}

// SQLRestorationParamsPublisher records a replay-stable, live-checked intent
// before object IO. The same intent is rechecked after publication. Only
// donor-mode glare is enabled by the corresponding SQL scope.
type SQLRestorationParamsPublisher struct {
	DB        v2portrait.DonorConfirmationTransactions
	Publisher v2input.DocumentPublisher
}

func (s SQLRestorationParamsPublisher) PublishDonorGlare(ctx context.Context,
	who auth.Identity, tenant, run, workflow uuid.UUID, ordinal int,
	paramsRaw []byte, pin v2portrait.RestorationQualificationPin,
	origin RestorationManualOrigin) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Publisher == nil || tenant == uuid.Nil || run == uuid.Nil ||
		workflow == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		!isDocumentRef(origin.Root) || !isDocumentRef(origin.Params) ||
		!isDocumentRef(origin.TargetPixel) || !isDocumentRef(origin.ProtectedPixel) ||
		len(paramsRaw) < 2 || len(paramsRaw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	body, err := canonical.Bytes(paramsRaw)
	if err != nil || !bytes.Equal(body, paramsRaw) {
		return zero, v2input.ErrInvalid
	}
	if pin.Operation != "portrait.glare" {
		return zero, v2input.ErrInvalid
	}
	if _, err = pin.Compile(); err != nil {
		return zero, err
	}
	var fields struct {
		Mode         string                         `json:"mode"`
		Model        string                         `json:"model_lock_sha"`
		Calibration  v2input.Ref                    `json:"calibration"`
		Base         v2input.SessionVersionBinding  `json:"base"`
		Donor        *v2input.SessionVersionBinding `json:"donor"`
		GlassesMask  v2input.Ref                    `json:"glasses_mask"`
		Protected    v2input.Ref                    `json:"protected"`
		Confirmation v2input.Ref                    `json:"confirmation"`
		SourceFaces  v2input.Ref                    `json:"source_faces"`
		DonorFaces   v2input.Ref                    `json:"donor_faces"`
	}
	if json.Unmarshal(body, &fields) != nil || fields.Mode != "donor" ||
		fields.Donor == nil || fields.Model != pin.ModelLockSHA ||
		fields.Calibration != pin.Calibration {
		return zero, v2input.ErrInvalid
	}
	owner, err := v2input.TaskID(workflow, "portrait.glare", ordinal)
	if err != nil {
		return zero, err
	}
	key, err := v2input.NodeKey("portrait.glare", ordinal)
	if err != nil {
		return zero, err
	}
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	request, err := canonical.Marshal(restorationParamsIntent{who.UserID, who.SessionID,
		key, origin, owner, string(body), sha, pin})
	if err != nil {
		return zero, err
	}
	if err = s.saveIntent(ctx, tenant, run, request); err != nil {
		return zero, err
	}
	ref, err := s.Publisher.Publish(ctx, artifactjson.Scope{Tenant: tenant, Run: run,
		Owner: owner, Purpose: "task_params", RestorationPortrait: true},
		append([]byte(nil), body...))
	if err != nil {
		return zero, err
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	if ref.ID == uuid.Nil || ref.SHA256 != sha ||
		ref.ID == origin.Root.ArtifactID || ref.ID == origin.Params.ArtifactID ||
		ref.ID == origin.TargetPixel.ArtifactID || ref.ID == origin.ProtectedPixel.ArtifactID {
		return zero, v2input.ErrInvalid
	}
	for _, input := range []v2input.Ref{fields.Base.Source, fields.Donor.Source,
		fields.GlassesMask, fields.Protected, fields.Confirmation,
		fields.SourceFaces, fields.DonorFaces, fields.Calibration, pin.Report} {
		if ref.ID == input.ArtifactID {
			return zero, v2input.ErrInvalid
		}
	}
	if err = s.saveIntent(ctx, tenant, run, request); err != nil {
		return zero, err
	}
	return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, nil
}

// PublishReflection uses the same fenced immutable document protocol as donor
// glare, but the SQL request scope independently verifies reflection's frozen
// draft, current manual origin and qualified model without a donor receipt.
func (s SQLRestorationParamsPublisher) PublishReflection(ctx context.Context,
	who auth.Identity, tenant, run, workflow uuid.UUID, ordinal int,
	paramsRaw []byte, pin v2portrait.RestorationQualificationPin,
	origin RestorationManualOrigin) (v2input.Ref, error) {
	var zero v2input.Ref
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Publisher == nil || tenant == uuid.Nil || run == uuid.Nil ||
		workflow == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		!isDocumentRef(origin.Root) || !isDocumentRef(origin.Params) ||
		!isDocumentRef(origin.TargetPixel) || !isDocumentRef(origin.ProtectedPixel) ||
		pin.Operation != "image.reflection" || len(paramsRaw) < 2 || len(paramsRaw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	if _, err := pin.Compile(); err != nil {
		return zero, err
	}
	body, err := canonical.Bytes(paramsRaw)
	if err != nil || !bytes.Equal(body, paramsRaw) {
		return zero, v2input.ErrInvalid
	}
	var fields struct {
		SchemaVersion int                           `json:"schema_version"`
		Base          v2input.SessionVersionBinding `json:"base"`
		Strength      float64                       `json:"strength"`
		Mask          v2input.Ref                   `json:"mask"`
		Protected     v2input.Ref                   `json:"protected"`
		ModelLockSHA  string                        `json:"model_lock_sha"`
		Calibration   v2input.Ref                   `json:"calibration"`
	}
	if json.Unmarshal(body, &fields) != nil || fields.SchemaVersion != 1 ||
		fields.ModelLockSHA != pin.ModelLockSHA || fields.Calibration != pin.Calibration {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(fields)
	if err != nil || !bytes.Equal(exact, body) {
		return zero, v2input.ErrInvalid
	}
	request := ReflectionPreparationRequest{SchemaVersion: 1, Snapshot: uuid.NameSpaceOID,
		Ordinal: ordinal, Base: fields.Base, Strength: fields.Strength,
		Mask: fields.Mask, Protected: fields.Protected,
		TargetPixel: origin.TargetPixel, ProtectedPixel: origin.ProtectedPixel}
	selectionRaw, err := canonical.Marshal(request)
	if err != nil {
		return zero, err
	}
	if _, err := DecodeReflectionPreparationRequest(selectionRaw); err != nil {
		return zero, err
	}
	owner, err := v2input.TaskID(workflow, "image.reflection", ordinal)
	if err != nil {
		return zero, err
	}
	key, err := v2input.NodeKey("image.reflection", ordinal)
	if err != nil {
		return zero, err
	}
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	requestRaw, err := canonical.Marshal(restorationParamsIntent{who.UserID, who.SessionID,
		key, origin, owner, string(body), sha, pin})
	if err != nil {
		return zero, err
	}
	if err := s.saveIntent(ctx, tenant, run, requestRaw); err != nil {
		return zero, err
	}
	ref, err := s.Publisher.Publish(ctx, artifactjson.Scope{Tenant: tenant, Run: run,
		Owner: owner, Purpose: "task_params", RestorationPortrait: true},
		append([]byte(nil), body...))
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if ref.ID == uuid.Nil || ref.SHA256 != sha {
		return zero, v2input.ErrInvalid
	}
	for _, input := range []v2input.Ref{fields.Base.Source, fields.Mask,
		fields.Protected, fields.Calibration, pin.Report, origin.Root,
		origin.Params, origin.TargetPixel, origin.ProtectedPixel} {
		if ref.ID == input.ArtifactID {
			return zero, v2input.ErrInvalid
		}
	}
	if err := s.saveIntent(ctx, tenant, run, requestRaw); err != nil {
		return zero, err
	}
	return v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}, nil
}

func (s SQLRestorationParamsPublisher) saveIntent(ctx context.Context,
	tenant, run uuid.UUID, request []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	if tx == nil {
		return v2input.ErrInvalid
	}
	defer rollbackDonorRecovery(ctx, tx)
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return err
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return err
	}
	if role != "photo_scheduler" {
		return v2input.ErrInvalid
	}
	var ready bool
	if err = tx.QueryRow(ctx,
		"SELECT photo.save_v28_restoration_params_intent($1,$2,$3::jsonb)",
		tenant, run, string(request)).Scan(&ready); err != nil {
		return err
	}
	if !ready {
		return v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
