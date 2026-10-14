package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2observation"
)

// GlareDonorIssuerParams contains the donor-mode glare fields needed by the
// trusted open-eyes issuer. The operation's remaining Params are compiled by
// the restoration workflow and cannot be inferred from this receipt.
type GlareDonorIssuerParams struct {
	Base         v2input.SessionVersionBinding
	Donor        v2input.SessionVersionBinding
	Strength     float64
	GlassesMask  v2input.Ref
	Protected    v2input.Ref
	Confirmation v2input.Ref
	SourceFaces  v2input.Ref
	DonorFaces   v2input.Ref
	SourceFaceID uuid.UUID
	DonorFaceID  uuid.UUID
}

func parseGlareDonorIssuerParams(paramsRaw []byte) (GlareDonorIssuerParams, error) {
	var zero GlareDonorIssuerParams
	if len(paramsRaw) < 2 || len(paramsRaw) > 65536 {
		return zero, v2input.ErrInvalid
	}
	canonicalRaw, err := canonical.Bytes(paramsRaw)
	if err != nil || !bytes.Equal(canonicalRaw, paramsRaw) {
		return zero, v2input.ErrInvalid
	}
	var p struct {
		SchemaVersion int                            `json:"schema_version"`
		Mode          string                         `json:"mode"`
		Base          v2input.SessionVersionBinding  `json:"base"`
		Donor         *v2input.SessionVersionBinding `json:"donor"`
		Strength      float64                        `json:"strength"`
		GlassesMask   v2input.Ref                    `json:"glasses_mask"`
		Protected     v2input.Ref                    `json:"protected"`
		Confirmation  v2input.Ref                    `json:"confirmation"`
		SourceFaces   v2input.Ref                    `json:"source_faces"`
		DonorFaces    v2input.Ref                    `json:"donor_faces"`
		ModelLockSHA  string                         `json:"model_lock_sha"`
		Calibration   v2input.Ref                    `json:"calibration"`
	}
	if json.Unmarshal(paramsRaw, &p) != nil || p.SchemaVersion != 1 ||
		p.Mode != "donor" || p.Donor == nil {
		return zero, v2input.ErrInvalid
	}
	typedRaw, err := canonical.Marshal(p)
	if err != nil || !bytes.Equal(typedRaw, paramsRaw) {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(p.ModelLockSHA); err != nil {
		return zero, v2input.ErrInvalid
	}
	if p.Calibration.ArtifactID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(p.Calibration.SHA256); err != nil {
		return zero, v2input.ErrInvalid
	}
	for _, ref := range []v2input.Ref{p.Base.Source, p.Donor.Source,
		p.GlassesMask, p.Protected, p.Confirmation, p.SourceFaces, p.DonorFaces} {
		if ref.ArtifactID == p.Calibration.ArtifactID {
			return zero, v2input.ErrInvalid
		}
	}
	return GlareDonorIssuerParams{Base: p.Base, Donor: *p.Donor,
		Strength: p.Strength, GlassesMask: p.GlassesMask,
		Protected: p.Protected, Confirmation: p.Confirmation,
		SourceFaces: p.SourceFaces, DonorFaces: p.DonorFaces}, nil
}

// DeriveGlareDonorIssuerParams takes face IDs only from exact, source-bound
// observation documents. The caller must obtain these bytes through a scoped,
// version-pinned catalog read; SQL rechecks provenance and consent on issue.
func DeriveGlareDonorIssuerParams(paramsRaw, sourceFacesRaw, donorFacesRaw []byte,
	sourceObservation, donorObservation uuid.UUID) (GlareDonorIssuerParams, error) {
	var zero GlareDonorIssuerParams
	if sourceObservation == uuid.Nil || donorObservation == uuid.Nil ||
		sourceObservation == donorObservation {
		return zero, v2input.ErrInvalid
	}
	result, err := parseGlareDonorIssuerParams(paramsRaw)
	if err != nil {
		return zero, err
	}
	binding := func(b v2input.SessionVersionBinding) v2observation.Binding {
		return v2observation.Binding{AssetVersionID: b.AssetVersionID,
			Source: v2observation.ArtifactRef{ArtifactID: b.Source.ArtifactID,
				SHA256: b.Source.SHA256}, CanvasHash: b.CanvasHash}
	}
	sourceFace, err := v2observation.ResolveFaceID(sourceFacesRaw,
		result.SourceFaces.SHA256, binding(result.Base), sourceObservation)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	donorFace, err := v2observation.ResolveFaceID(donorFacesRaw,
		result.DonorFaces.SHA256, binding(result.Donor), donorObservation)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	result.SourceFaceID, result.DonorFaceID = sourceFace, donorFace
	if _, err := result.openEyesParams(); err != nil {
		return zero, err
	}
	return result, nil
}

// BindGlareDonorConfirmation replaces the owner placeholder with the issued
// document ref. Every other field remains unchanged.
func BindGlareDonorConfirmation(paramsRaw []byte, original,
	issued GlareDonorIssuerParams) ([]byte, error) {
	canonicalRaw, err := canonical.Bytes(paramsRaw)
	if err != nil || !bytes.Equal(canonicalRaw, paramsRaw) {
		return nil, v2input.ErrInvalid
	}
	if original.Confirmation.ArtifactID == uuid.Nil ||
		issued.Confirmation.ArtifactID == uuid.Nil ||
		issued.Confirmation.ArtifactID == original.Confirmation.ArtifactID ||
		issued.Confirmation.SHA256 == "" {
		return nil, v2input.ErrInvalid
	}
	for _, ref := range []v2input.Ref{original.Base.Source, original.Donor.Source,
		original.GlassesMask, original.Protected, original.SourceFaces, original.DonorFaces} {
		if issued.Confirmation.ArtifactID == ref.ArtifactID {
			return nil, v2input.ErrInvalid
		}
	}
	if _, err := domain.ParseDigest(issued.Confirmation.SHA256); err != nil {
		return nil, v2input.ErrInvalid
	}
	check := issued
	check.Confirmation = original.Confirmation
	if check != original {
		return nil, v2input.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(paramsRaw, &fields) != nil || fields == nil {
		return nil, v2input.ErrInvalid
	}
	var old v2input.Ref
	if json.Unmarshal(fields["confirmation"], &old) != nil ||
		old != original.Confirmation {
		return nil, v2input.ErrInvalid
	}
	ref, err := canonical.Marshal(issued.Confirmation)
	if err != nil {
		return nil, err
	}
	fields["confirmation"] = ref
	return canonical.Marshal(fields)
}

// IssueGlareDonorForRestorationParams is the server composition point before
// Params publication. Observation bytes must come from current scoped reads;
// graph admission later repeats SQL authority over the final document.
func IssueGlareDonorForRestorationParams(ctx context.Context,
	db DonorConfirmationTransactions, publisher DonorConfirmationPublisher,
	who auth.Identity, tenant, project, shoot, run uuid.UUID,
	paramsRaw, sourceFacesRaw, donorFacesRaw []byte,
	sourceObservation, donorObservation uuid.UUID) ([]byte, error) {
	original, err := DeriveGlareDonorIssuerParams(paramsRaw, sourceFacesRaw,
		donorFacesRaw, sourceObservation, donorObservation)
	if err != nil {
		return nil, err
	}
	issued, err := IssueGlareDonorConfirmation(ctx, db, publisher, who,
		tenant, project, shoot, run, original, sourceObservation, donorObservation)
	if err != nil {
		return nil, err
	}
	final, err := BindGlareDonorConfirmation(paramsRaw, original, issued)
	if err != nil {
		return nil, err
	}
	current, err := DeriveGlareDonorIssuerParams(final, sourceFacesRaw,
		donorFacesRaw, sourceObservation, donorObservation)
	if err != nil || current != issued {
		return nil, v2input.ErrInvalid
	}
	return final, nil
}

// IssueGlareDonorFromObservations uses the scheduler-only SQL face choice when
// face documents cannot be read before the confirmation has been issued.
// The issuer rechecks the same selected observations before and after object IO.
func IssueGlareDonorFromObservations(ctx context.Context,
	db DonorConfirmationTransactions, publisher DonorConfirmationPublisher,
	who auth.Identity, tenant, project, shoot, run uuid.UUID,
	paramsRaw []byte, sourceObservation, donorObservation uuid.UUID) ([]byte, error) {
	original, err := parseGlareDonorIssuerParams(paramsRaw)
	if err != nil {
		return nil, err
	}
	choice, err := ReadGlareDonorFaceChoice(ctx, db, who, tenant, project,
		shoot, run, paramsRaw, sourceObservation, donorObservation)
	if err != nil {
		return nil, err
	}
	original.SourceFaceID, original.DonorFaceID = choice.SourceFaceID, choice.DonorFaceID
	if _, err := original.openEyesParams(); err != nil {
		return nil, err
	}
	issued, err := IssueGlareDonorConfirmation(ctx, db, publisher, who,
		tenant, project, shoot, run, original, sourceObservation, donorObservation)
	if err != nil {
		return nil, err
	}
	final, err := BindGlareDonorConfirmation(paramsRaw, original, issued)
	if err != nil {
		return nil, err
	}
	current, err := parseGlareDonorIssuerParams(final)
	if err != nil {
		return nil, err
	}
	current.SourceFaceID, current.DonorFaceID = choice.SourceFaceID, choice.DonorFaceID
	if current != issued {
		return nil, v2input.ErrInvalid
	}
	return final, nil
}

func (p GlareDonorIssuerParams) openEyesParams() (OpenEyesParams, error) {
	open := OpenEyesParams{SchemaVersion: 1, Base: p.Base, Strength: p.Strength,
		Donor: p.Donor, SourceFaceID: p.SourceFaceID, DonorFaceID: p.DonorFaceID,
		EyesMask: p.GlassesMask, Protected: p.Protected,
		Confirmation: p.Confirmation, SourceFaces: p.SourceFaces,
		DonorFaces: p.DonorFaces}
	// The confirmation, masks and observation documents must be distinct
	// before the issuer opens a transaction or publishes an object.
	if _, err := DonorDirectInputs(open); err != nil {
		return OpenEyesParams{}, err
	}
	return open, nil
}

// IssueGlareDonorConfirmation publishes in the restoration run and retains
// the existing server-side identity, consent, object-version and retry gates.
// Callers must obtain face/observation IDs from current scoped evidence.
func IssueGlareDonorConfirmation(ctx context.Context, db DonorConfirmationTransactions,
	publisher DonorConfirmationPublisher, who auth.Identity,
	tenant, project, shoot, run uuid.UUID, params GlareDonorIssuerParams,
	sourceObservation, donorObservation uuid.UUID) (GlareDonorIssuerParams, error) {
	var zero GlareDonorIssuerParams
	open, err := params.openEyesParams()
	if err != nil {
		return zero, err
	}
	bound, err := IssueOpenEyesConfirmation(ctx, db, publisher, who,
		tenant, project, shoot, run, open, sourceObservation, donorObservation)
	if err != nil {
		return zero, err
	}
	params.Confirmation = bound.Confirmation
	return params, nil
}

// IssueGlare uses the same SQL repository and object publisher as open-eyes.
func (s OpenEyesConfirmationService) IssueGlare(ctx context.Context, who auth.Identity,
	tenant, project, shoot, run uuid.UUID, params GlareDonorIssuerParams,
	sourceObservation, donorObservation uuid.UUID) (GlareDonorIssuerParams, error) {
	if s.DB == nil || s.Objects == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return GlareDonorIssuerParams{}, v2input.ErrInvalid
	}
	repository := SQLDonorConfirmationRepository{DB: s.DB, Bucket: s.Bucket}
	publisher := DonorConfirmationObjectPublisher{Repository: repository, Objects: s.Objects}
	return IssueGlareDonorConfirmation(ctx, s.DB, publisher, who, tenant,
		project, shoot, run, params, sourceObservation, donorObservation)
}

// IssueGlareParams composes the configured SQL issuer and object publisher for
// a canonical restoration Params proposal. It returns the exact Params bytes
// to publish after the server-issued confirmation has become ready.
func (s OpenEyesConfirmationService) IssueGlareParams(ctx context.Context,
	who auth.Identity, tenant, project, shoot, run uuid.UUID,
	paramsRaw, sourceFacesRaw, donorFacesRaw []byte,
	sourceObservation, donorObservation uuid.UUID) ([]byte, error) {
	if s.DB == nil || s.Objects == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return nil, v2input.ErrInvalid
	}
	repository := SQLDonorConfirmationRepository{DB: s.DB, Bucket: s.Bucket}
	publisher := DonorConfirmationObjectPublisher{Repository: repository, Objects: s.Objects}
	return IssueGlareDonorForRestorationParams(ctx, s.DB, publisher, who,
		tenant, project, shoot, run, paramsRaw, sourceFacesRaw, donorFacesRaw,
		sourceObservation, donorObservation)
}

// IssueGlareParamsFromObservations composes the SQL face choice, existing
// confirmation publisher, and exact final Params binding for the draft run.
func (s OpenEyesConfirmationService) IssueGlareParamsFromObservations(ctx context.Context,
	who auth.Identity, tenant, project, shoot, run uuid.UUID, paramsRaw []byte,
	sourceObservation, donorObservation uuid.UUID) ([]byte, error) {
	if s.DB == nil || s.Objects == nil || len(s.Bucket) < 3 || len(s.Bucket) > 63 {
		return nil, v2input.ErrInvalid
	}
	repository := SQLDonorConfirmationRepository{DB: s.DB, Bucket: s.Bucket}
	publisher := DonorConfirmationObjectPublisher{Repository: repository, Objects: s.Objects}
	return IssueGlareDonorFromObservations(ctx, s.DB, publisher, who,
		tenant, project, shoot, run, paramsRaw, sourceObservation, donorObservation)
}
