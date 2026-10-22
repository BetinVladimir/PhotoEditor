package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/v2input"
	"time"
)

// RetouchPreviewLookup is server-resolved state, never a client-supplied run or
// source document. Resolution may resume a separately admitted proposal render.
type RetouchPreviewLookup struct {
	RunID          uuid.UUID
	Params         RetouchPreviewParams
	SourceRaw      []byte
	AdapterContext v2input.Ref
}

type RetouchPreviewResolver func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (RetouchPreviewLookup, error)

type RetouchPreviewProvider struct {
	db                  *pgxpool.Pool
	store               planObjectStore
	bucket              string
	resolve             RetouchPreviewResolver
	sign                PreviewURLSigner
	iccLockSHA, srgbSHA string
}

func NewRetouchPreviewProvider(db *pgxpool.Pool, store planObjectStore, bucket string,
	resolve RetouchPreviewResolver, sign PreviewURLSigner, iccLockSHA, srgbSHA string) (*RetouchPreviewProvider, error) {
	if db == nil || store == nil || bucket == "" || resolve == nil || sign == nil ||
		!shaPattern.MatchString(iccLockSHA) || !shaPattern.MatchString(srgbSHA) {
		return nil, ErrInvalid
	}
	return &RetouchPreviewProvider{db: db, store: store, bucket: bucket, resolve: resolve, sign: sign,
		iccLockSHA: iccLockSHA, srgbSHA: srgbSHA}, nil
}

// ReadPreview implements the HTTP provider contract without borrowing Apply
// authority. All resolver output is re-bound to the authenticated request.
func (p *RetouchPreviewProvider) ReadPreview(ctx context.Context, who auth.Identity,
	tenant, intent, target uuid.UUID, request PreviewRequest) (Preview, error) {
	physical, _, err := p.ReadPhysicalPreview(ctx, who, tenant, intent, target, request)
	if err != nil {
		return Preview{}, err
	}
	authority := func(ctx context.Context) error {
		return CheckCurrentPreviewAuthority(ctx, p.db, who, tenant, intent, target, request)
	}
	return BuildRetouchPreviewResponse(ctx, physical, request, time.Now(), p.sign, authority)
}

// ReadPhysicalPreview verifies artifacts and seals the exact canonical receipt.
// Apply still requires its own transaction and selection locks.
func (p *RetouchPreviewProvider) ReadPhysicalPreview(ctx context.Context, who auth.Identity,
	tenant, intent, target uuid.UUID, request PreviewRequest) (PhysicalRetouchPreview, LockedRetouchPreviewSelection, error) {
	empty := PhysicalRetouchPreview{}
	if err := ctx.Err(); err != nil {
		return empty, LockedRetouchPreviewSelection{}, err
	}
	if p == nil || p.db == nil || p.resolve == nil || p.sign == nil || p.store == nil {
		return empty, LockedRetouchPreviewSelection{}, ErrUnavailable
	}
	authority := func(ctx context.Context) error {
		return CheckCurrentPreviewAuthority(ctx, p.db, who, tenant, intent, target, request)
	}
	if err := authority(ctx); err != nil {
		return empty, LockedRetouchPreviewSelection{}, err
	}
	lookup, err := p.resolve(ctx, who, tenant, intent, target, request)
	if err != nil {
		return empty, LockedRetouchPreviewSelection{}, err
	}
	if err := ctx.Err(); err != nil {
		return empty, LockedRetouchPreviewSelection{}, err
	}
	params := lookup.Params
	lookup.SourceRaw = append([]byte(nil), lookup.SourceRaw...)
	if _, _, err := FreezeRetouchPreviewParams(params); err != nil {
		return empty, LockedRetouchPreviewSelection{}, err
	}
	if lookup.AdapterContext.ArtifactID == uuid.Nil || !shaPattern.MatchString(lookup.AdapterContext.SHA256) || lookup.RunID == uuid.Nil || params.TenantID != tenant || params.IntentID != intent || params.TargetID != target ||
		params.WorkflowRevision != request.ExpectedRevision || params.TargetRevision != request.TargetRevision ||
		params.Plan.SHA256 != request.PlanSHA || params.Candidate.SHA256 != request.CandidateSHA {
		return empty, LockedRetouchPreviewSelection{}, ErrConflict
	}
	if err := authority(ctx); err != nil {
		return empty, LockedRetouchPreviewSelection{}, err
	}
	physical, err := ReadPhysicalRetouchPreview(ctx, p.db, p.store, p.bucket, who, lookup.RunID, params, lookup.AdapterContext,
		append([]byte(nil), lookup.SourceRaw...), p.iccLockSHA, p.srgbSHA)
	if err != nil {
		return empty, LockedRetouchPreviewSelection{}, err
	}
	receipt, err := SealRetouchPreviewReceipt(ctx, p.db, who, lookup.RunID, physical)
	if err != nil {
		return empty, LockedRetouchPreviewSelection{}, err
	}
	if err = authority(ctx); err != nil {
		return empty, LockedRetouchPreviewSelection{}, err
	}
	if err := ctx.Err(); err != nil {
		return empty, LockedRetouchPreviewSelection{}, err
	}
	return physical, LockedRetouchPreviewSelection{TargetID: target, PreviewRunID: lookup.RunID, Receipt: receipt}, nil
}

// LockedRetouchPreviewSelection is server-resolved producer linkage. It does
// not assert that the Apply transaction has acquired any locks yet.
type LockedRetouchPreviewSelection struct {
	TargetID     uuid.UUID
	PreviewRunID uuid.UUID
	Receipt      RetouchPreviewReceipt
}
