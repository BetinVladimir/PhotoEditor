package semanticproposal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// ProjectCreateService selects deployment policy only after resolving the
// authenticated request's original inventory. Its private catalog is immutable;
// caller-owned mutable policy slices/maps are detached at construction.
type ProjectCreateService struct {
	api      *pgxpool.Pool
	projects map[uuid.UUID]CreateService
}

func NewProjectCreateService(ctx context.Context, preparers []TransferDraftPreparer) (*ProjectCreateService, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(preparers) < 1 || len(preparers) > 4096 {
		return nil, v2input.ErrInvalid
	}
	service := &ProjectCreateService{projects: make(map[uuid.UUID]CreateService, len(preparers))}
	for _, p := range preparers {
		p.Configuration = ownTransferPreparation(p.Configuration)
		if p.Configuration.PersonProtection == nil || p.Configuration.SourceProtection != (v2input.Ref{}) || len(p.Configuration.TargetProtection) != 0 {
			return nil, v2input.ErrInvalid
		}
		if err := validateTransferPolicyConfiguration(p.Configuration); err != nil {
			return nil, err
		}
		if err := validateCreatePreparer(ctx, p); err != nil {
			return nil, err
		}
		if service.api == nil {
			service.api = p.Reader.DB
		}
		if p.Reader.DB != service.api {
			return nil, v2input.ErrInvalid
		}
		if _, exists := service.projects[p.Reader.ProjectID]; exists {
			return nil, v2input.ErrConflict
		}
		service.projects[p.Reader.ProjectID] = CreateService{Preparer: p}
	}
	return service, nil
}

func (s *ProjectCreateService) Propose(ctx context.Context, who auth.Identity, tenant uuid.UUID, key string, intent v2input.SemanticEditIntent) (idempotency.Response, error) {
	var zero idempotency.Response
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s == nil || s.api == nil {
		return zero, v2input.ErrInvalid
	}
	raw, err := canonical.Marshal(intent)
	if err != nil {
		return zero, err
	}
	request, err := CompileRequest(tenant, who.UserID, key, raw)
	if err != nil {
		return zero, err
	}
	if replay,found,e:=replayCreateReceipt(ctx,s.api,who,request,key);e!=nil{return zero,e}else if found{return replay,nil}
	inventory, err := ReadInventory(ctx, s.api, who, request)
	if err != nil {
		return zero, err
	}
	service, exists := s.projects[inventory.Scope.ProjectID]
	if !exists {
		return zero, v2qualification.ErrUnqualified
	}
	return service.Propose(ctx, who, tenant, key, intent)
}
