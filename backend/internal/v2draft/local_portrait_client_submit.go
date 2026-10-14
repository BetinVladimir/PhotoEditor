package v2draft

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

type LocalPortraitTicket struct {
	RunID      uuid.UUID `json:"run_id"`
	WorkflowID uuid.UUID `json:"workflow_id"`
	TaskID     uuid.UUID `json:"task_id"`
	State      string    `json:"state"`
}

// A project-scoped adapter from unprivileged editor choices to the existing
// durable owner pipeline. The SQL owner and admission gates remain authoritative.
type LocalPortraitSubmissionService struct {
	Scheduler        *pgxpool.Pool
	Catalog          *pgxpool.Pool
	Objects          v2input.SessionAnalysisObjectReader
	Documents        v2input.DocumentPublisher
	Bucket           string
	Tenant           uuid.UUID
	Project          uuid.UUID
	RuntimeSHA       string
	PolicySHA        string
	MaxReservedBytes int64
}

func (s LocalPortraitSubmissionService) Valid() bool {
	if s.Scheduler == nil || s.Catalog == nil || s.Scheduler == s.Catalog || s.Objects == nil || s.Documents == nil || s.Bucket == "" || s.Tenant == uuid.Nil || s.Project == uuid.Nil || s.MaxReservedBytes < 1 || s.MaxReservedBytes > 9007199254740991 {
		return false
	}
	if _, err := domain.ParseDigest(s.RuntimeSHA); err != nil {
		return false
	}
	if _, err := domain.ParseDigest(s.PolicySHA); err != nil {
		return false
	}
	return true
}

func (s LocalPortraitSubmissionService) PrepareProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	request LocalPortraitClientSelection) (LocalPortraitClientSelection, error) {
	if !s.Valid() {
		return LocalPortraitClientSelection{}, v2input.ErrInvalid
	}
	return (LocalPortraitClientPreparationService{Scheduler: s.Scheduler, Tenant: s.Tenant, Project: s.Project}).PrepareProjectKeyed(ctx, who, tenant, project, key, request)
}

func (s LocalPortraitSubmissionService) SubmitProjectKeyed(ctx context.Context,
	who auth.Identity, tenant, project uuid.UUID, key string,
	request LocalPortraitClientSelection) (LocalPortraitTicket, error) {
	var zero LocalPortraitTicket
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if !s.Valid() || tenant != s.Tenant || project != s.Project || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	request, err := s.PrepareProjectKeyed(ctx, who, tenant, project, key, request)
	if err != nil {
		return zero, err
	}
	run, err := v2input.RunID(tenant, who.UserID, key)
	if err != nil {
		return zero, err
	}
	var params struct {
		Base      v2input.SessionVersionBinding `json:"base"`
		Mask      v2input.Ref                   `json:"mask"`
		Protected v2input.Ref                   `json:"protected"`
	}
	if json.Unmarshal(request.Params, &params) != nil {
		return zero, v2input.ErrInvalid
	}
	resolver := ClosureResolver{Catalog: SQLArtifactCatalog{DB: s.Catalog, Bucket: s.Bucket, ProjectID: project, AssetVersionID: params.Base.AssetVersionID}, Objects: s.Objects}
	source, err := resolver.ReadLocalPortraitSource(ctx, who, tenant, params.Base)
	if err != nil {
		return zero, err
	}
	documents, err := v2portrait.ReadManualPairDocuments(ctx, s.Scheduler, s.Objects, who, tenant, project, request.Snapshot, params.Base, source, params.Mask, params.Protected, request.TargetPixel, request.ProtectedPixel, s.Bucket)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		return zero, err
	}
	capacity, err := v2portrait.LocalPortraitResources(image.Canvas.Width, image.Canvas.Height)
	if err != nil {
		return zero, err
	}
	const overhead int64 = 65536 + 2097152
	if capacity.OutputBytes > s.MaxReservedBytes-overhead {
		return zero, v2input.ErrInvalid
	}
	reserved := capacity.OutputBytes + overhead
	proof := LocalPortraitRecoveryProof{Root: documents.Origin.Root, RootRaw: documents.RootRaw, OriginalParams: documents.Origin.Params, OriginalRaw: documents.ParamsRaw, VectorsRaw: documents.VectorsRaw}
	owner, _, err := resolver.CompileLocalPortraitOwnerRequest(ctx, who, tenant, project, request.Snapshot, run, request.Ordinal, request.Operation, params.Base, source, request.Params, s.RuntimeSHA, capacity, proof)
	if err != nil {
		return zero, err
	}
	service, err := NewSQLLocalPortraitRecoveryService(s.Scheduler, s.Catalog, s.Bucket, s.Objects, s.Documents, s.RuntimeSHA, capacity)
	if err != nil {
		return zero, err
	}
	saved, readErr := (SQLLocalPortraitOwnerScope{DB: s.Scheduler}).Load(ctx, who, tenant, run)
	var admitted LocalPortraitAdmission
	switch {
	case readErr == nil:
		if !bytes.Equal(saved.Request, owner) || saved.PolicySHA != s.PolicySHA || saved.ReservedBytes != reserved {
			return zero, v2input.ErrConflict
		}
		admitted, err = service.Resume(ctx, who, tenant, run)
	case errors.Is(readErr, v2input.ErrExpired):
		admitted, err = service.Submit(ctx, who, tenant, owner, s.PolicySHA, reserved)
	default:
		return zero, readErr
	}
	if err != nil {
		return zero, err
	}
	workflow, err := v2input.WorkflowID(run)
	if err != nil || admitted.Run != run || admitted.Workflow != workflow {
		return zero, v2input.ErrInvalid
	}
	task, err := v2input.TaskID(workflow, request.Operation, request.Ordinal)
	if err != nil || admitted.Task != task {
		return zero, v2input.ErrInvalid
	}
	return LocalPortraitTicket{RunID: run, WorkflowID: workflow, TaskID: task, State: "queued"}, nil
}
