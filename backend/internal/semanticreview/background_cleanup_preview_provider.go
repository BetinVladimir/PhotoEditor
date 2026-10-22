package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"time"
)

// BackgroundCleanupPreviewLookup is server-resolved state, never a client-supplied run or
// source document. Resolution may resume a separately admitted proposal render.
type BackgroundCleanupPreviewLookup struct {
	RunID     uuid.UUID
	Params    BackgroundCleanupPreviewParams
	SourceRaw []byte
}

type BackgroundCleanupPreviewResolver func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (BackgroundCleanupPreviewLookup, error)

type BackgroundCleanupPreviewProvider struct {
	db                  *pgxpool.Pool
	store               planObjectStore
	bucket              string
	resolve             BackgroundCleanupPreviewResolver
	sign                PreviewURLSigner
	iccLockSHA, srgbSHA string
}

func NewBackgroundCleanupPreviewProvider(db *pgxpool.Pool, store planObjectStore, bucket string,
	resolve BackgroundCleanupPreviewResolver, sign PreviewURLSigner, iccLockSHA, srgbSHA string) (*BackgroundCleanupPreviewProvider, error) {
	if db == nil || store == nil || bucket == "" || resolve == nil || sign == nil ||
		!shaPattern.MatchString(iccLockSHA) || !shaPattern.MatchString(srgbSHA) {
		return nil, ErrInvalid
	}
	return &BackgroundCleanupPreviewProvider{db: db, store: store, bucket: bucket, resolve: resolve, sign: sign,
		iccLockSHA: iccLockSHA, srgbSHA: srgbSHA}, nil
}

// ReadPreview implements the HTTP provider contract without borrowing Apply
// authority. All resolver output is re-bound to the authenticated request.
func (p *BackgroundCleanupPreviewProvider) ReadPreview(ctx context.Context, who auth.Identity,
	tenant, intent, target uuid.UUID, request PreviewRequest) (Preview, error) {
	physical, _, err := p.ReadPhysicalPreview(ctx, who, tenant, intent, target, request)
	if err != nil {
		return Preview{}, err
	}
	authority := func(ctx context.Context) error {
		return CheckCurrentPreviewAuthority(ctx, p.db, who, tenant, intent, target, request)
	}
	return BuildBackgroundCleanupPreviewResponse(ctx, physical, request, time.Now(), p.sign, authority)
}

// ReadPhysicalPreview verifies artifacts and seals the exact canonical receipt.
// Apply still requires its own transaction and selection locks.
func (p *BackgroundCleanupPreviewProvider) ReadPhysicalPreview(ctx context.Context, who auth.Identity,
	tenant, intent, target uuid.UUID, request PreviewRequest) (PhysicalBackgroundCleanupPreview, LockedBackgroundCleanupPreviewSelection, error) {
	empty := PhysicalBackgroundCleanupPreview{}
	if err := ctx.Err(); err != nil {
		return empty, LockedBackgroundCleanupPreviewSelection{}, err
	}
	if p == nil || p.db == nil || p.resolve == nil || p.sign == nil || p.store == nil {
		return empty, LockedBackgroundCleanupPreviewSelection{}, ErrUnavailable
	}
	authority := func(ctx context.Context) error {
		return CheckCurrentPreviewAuthority(ctx, p.db, who, tenant, intent, target, request)
	}
	if err := authority(ctx); err != nil {
		return empty, LockedBackgroundCleanupPreviewSelection{}, err
	}
	lookup, err := p.resolve(ctx, who, tenant, intent, target, request)
	if err != nil {
		return empty, LockedBackgroundCleanupPreviewSelection{}, err
	}
	if err := ctx.Err(); err != nil {
		return empty, LockedBackgroundCleanupPreviewSelection{}, err
	}
	params := lookup.Params
	lookup.SourceRaw = append([]byte(nil), lookup.SourceRaw...)
	if _, _, err := FreezeBackgroundCleanupPreviewParams(params); err != nil {
		return empty, LockedBackgroundCleanupPreviewSelection{}, err
	}
	if lookup.RunID == uuid.Nil || params.TenantID != tenant || params.IntentID != intent || params.TargetID != target ||
		params.WorkflowRevision != request.ExpectedRevision || params.TargetRevision != request.TargetRevision ||
		params.Plan.SHA256 != request.PlanSHA || params.Candidate.SHA256 != request.CandidateSHA {
		return empty, LockedBackgroundCleanupPreviewSelection{}, ErrConflict
	}
	if err := authority(ctx); err != nil {
		return empty, LockedBackgroundCleanupPreviewSelection{}, err
	}
	physical, err := ReadPhysicalBackgroundCleanupPreview(ctx, p.db, p.store, p.bucket, who, lookup.RunID, params,
		append([]byte(nil), lookup.SourceRaw...), p.iccLockSHA, p.srgbSHA)
	if err != nil {
		return empty, LockedBackgroundCleanupPreviewSelection{}, err
	}
	receipt, err := SealBackgroundCleanupPreviewReceipt(ctx, p.db, who, lookup.RunID, physical)
	if err != nil {
		return empty, LockedBackgroundCleanupPreviewSelection{}, err
	}
	if err = authority(ctx); err != nil {
		return empty, LockedBackgroundCleanupPreviewSelection{}, err
	}
	if err := ctx.Err(); err != nil {
		return empty, LockedBackgroundCleanupPreviewSelection{}, err
	}
	return physical, LockedBackgroundCleanupPreviewSelection{TargetID: target, PreviewRunID: lookup.RunID, Receipt: receipt}, nil
}

// LockedBackgroundCleanupPreviewSelection is server-resolved producer linkage. It does
// not assert that the Apply transaction has acquired any locks yet.
type LockedBackgroundCleanupPreviewSelection struct {
	TargetID     uuid.UUID
	PreviewRunID uuid.UUID
	Receipt      BackgroundCleanupPreviewReceipt
}
