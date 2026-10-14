package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

type RelightPreparationRequest struct {
	SchemaVersion int                           `json:"schema_version"`
	Snapshot      uuid.UUID                     `json:"snapshot_id"`
	Base          v2input.SessionVersionBinding `json:"base"`
	Depth         v2input.Ref                   `json:"depth"`
	Subject       v2input.Ref                   `json:"subject"`
	Protected     v2input.Ref                   `json:"protected"`
	SubjectKind   string                        `json:"subject_kind"`
	Ordinal       int                           `json:"ordinal"`
}

func DecodeRelightPreparationRequest(raw []byte) (RelightPreparationRequest, error) {
	var zero RelightPreparationRequest
	if len(raw) < 2 || len(raw) > 8192 {
		return zero, v2input.ErrInvalid
	}
	normal, err := canonical.Bytes(raw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	var request RelightPreparationRequest
	if json.Unmarshal(normal, &request) != nil || request.SchemaVersion != 1 || request.Ordinal < 0 || request.Ordinal >= 5000 || (request.SubjectKind != "person" && request.SubjectKind != "manual") {
		return zero, v2input.ErrInvalid
	}
	exact, err := canonical.Marshal(request)
	if err != nil || !bytes.Equal(exact, normal) {
		return zero, v2input.ErrInvalid
	}
	if err = (RelightCandidateQuery{Snapshot: request.Snapshot, Base: request.Base, Kind: "depth"}).validate(); err != nil {
		return zero, v2input.ErrInvalid
	}
	for _, ref := range []v2input.Ref{request.Depth, request.Subject, request.Protected} {
		if !validRelightTransportRef(ref) {
			return zero, v2input.ErrInvalid
		}
	}
	return request, nil
}

// PrepareAuthoredIntent returns editable controls from original scoped producer
// choices. It never creates a run or grants execution authority. Submission
// repeats full pixel, producer, quota and qualification checks.
func (s *RelightSubmissionService) PrepareAuthoredIntent(ctx context.Context, who auth.Identity, request RelightPreparationRequest) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s == nil {
		return nil, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(request)
	if err != nil {
		return nil, err
	}
	request, err = DecodeRelightPreparationRequest(raw)
	if err != nil {
		return nil, err
	}
	params := RelightParams{SchemaVersion: 1, Base: request.Base, Strength: .5, Depth: request.Depth, Subject: request.Subject, Protected: request.Protected, Calibration: s.pin.Calibration, Lights: []RelightLight{{X: .5, Y: .5, Radius: .3, EV: 0}}}
	if _, _, err = params.Compile(); err != nil {
		return nil, err
	}
	sourceReader := SQLRelightSourceReader{DB: s.coordinator.DB, Objects: s.coordinator.Objects, Bucket: s.coordinator.Bucket}
	source, err := sourceReader.Load(ctx, who, s.tenant, s.project, request.Snapshot, request.Base)
	if err != nil {
		return nil, err
	}
	if _, err = LoadRelightQualificationEvidence(ctx, SQLRelightQualificationCatalog{DB: s.coordinator.DB, Bucket: s.coordinator.Bucket}, s.coordinator.Objects, who, s.tenant, s.project, params, s.pin, s.coordinator.Expected); err != nil {
		return nil, err
	}
	reader := SQLRelightPreintentReader{DB: s.coordinator.DB, Objects: s.coordinator.Objects, Bucket: s.coordinator.Bucket}
	subject := "person"
	if request.SubjectKind == "manual" {
		subject = "manual_target"
	}
	for _, kind := range []string{"depth", subject, "manual_guard"} {
		if _, err = reader.Load(ctx, who, s.tenant, s.project, request.Snapshot, params, source, kind); err != nil {
			return nil, err
		}
	}
	// Repeat source and qualification locators after producer reads; final submit
	// remains responsible for atomic checks across every dependency.
	if _, err = sourceReader.lookup(ctx, who, s.tenant, s.project, request.Snapshot, request.Base); err != nil {
		return nil, err
	}
	if _, _, _, err = (SQLRelightQualificationCatalog{DB: s.coordinator.DB, Bucket: s.coordinator.Bucket}).ResolveRelightQualification(ctx, who, s.tenant, s.project, s.pin); err != nil {
		return nil, err
	}
	compiled, _, err := params.Compile()
	if err != nil {
		return nil, err
	}
	return canonical.Marshal(relightDraftIntentWire{SchemaVersion: 1, Snapshot: request.Snapshot, Ordinal: request.Ordinal, SubjectKind: request.SubjectKind, Params: compiled})
}
func (s *RelightProjectServices) PrepareProjectIntent(ctx context.Context, who auth.Identity, tenant, project uuid.UUID, request RelightPreparationRequest) ([]byte, error) {
	if s == nil {
		return nil, v2qualification.ErrUnqualified
	}
	service := s.services[[2]uuid.UUID{tenant, project}]
	if service == nil {
		return nil, v2qualification.ErrUnqualified
	}
	return service.PrepareAuthoredIntent(ctx, who, request)
}
