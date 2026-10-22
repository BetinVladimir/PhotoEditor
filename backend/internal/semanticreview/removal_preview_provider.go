package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"time"
)

// RemovalPreviewLookup is server-resolved state, never a client-supplied run or
// source document. Resolution may resume a separately admitted proposal render.
type RemovalPreviewLookup struct {
	RunID     uuid.UUID
	Params    RemovalPreviewParams
	SourceRaw []byte
}

type RemovalPreviewResolver func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (RemovalPreviewLookup, error)

type RemovalPreviewProvider struct {
	db                  *pgxpool.Pool
	store               planObjectStore
	bucket              string
	resolve             RemovalPreviewResolver
	sign                PreviewURLSigner
	iccLockSHA, srgbSHA string
}

func NewRemovalPreviewProvider(db *pgxpool.Pool, store planObjectStore, bucket string,
	resolve RemovalPreviewResolver, sign PreviewURLSigner, iccLockSHA, srgbSHA string) (*RemovalPreviewProvider, error) {
	if db == nil || store == nil || bucket == "" || resolve == nil || sign == nil ||
		!shaPattern.MatchString(iccLockSHA) || !shaPattern.MatchString(srgbSHA) {
		return nil, ErrInvalid
	}
	return &RemovalPreviewProvider{db: db, store: store, bucket: bucket, resolve: resolve, sign: sign,
		iccLockSHA: iccLockSHA, srgbSHA: srgbSHA}, nil
}

// ReadPreview implements the HTTP provider contract without borrowing Apply
// authority. All resolver output is re-bound to the authenticated request.
func (p *RemovalPreviewProvider) ReadPreview(ctx context.Context, who auth.Identity,
	tenant, intent, target uuid.UUID, request PreviewRequest) (Preview, error) {
	physical, selection, err := p.ReadPhysicalPreview(ctx, who, tenant, intent, target, request)
	if err != nil {
		return Preview{}, err
	}
	authority := func(ctx context.Context) error {
        if err := CheckCurrentPreviewAuthority(ctx, p.db, who, tenant, intent, target, request); err != nil {
            return err
        }
        return recheckRemovalIssuedPreview(ctx, p.db, p.store, p.bucket, who,
            selection.PreviewRunID, physical, p.iccLockSHA, p.srgbSHA)
	}
	return BuildRemovalPreviewResponse(ctx, physical, request, time.Now(), p.sign, authority)
}

// ReadPhysicalPreview verifies artifacts and seals the exact canonical receipt.
// Apply still requires its own transaction and selection locks.
func (p *RemovalPreviewProvider) ReadPhysicalPreview(ctx context.Context, who auth.Identity,
	tenant, intent, target uuid.UUID, request PreviewRequest) (PhysicalRemovalPreview, LockedRemovalPreviewSelection, error) {
	empty := PhysicalRemovalPreview{}
	if err := ctx.Err(); err != nil {
		return empty, LockedRemovalPreviewSelection{}, err
	}
	if p == nil || p.db == nil || p.resolve == nil || p.sign == nil || p.store == nil {
		return empty, LockedRemovalPreviewSelection{}, ErrUnavailable
	}
	authority := func(ctx context.Context) error {
		return CheckCurrentPreviewAuthority(ctx, p.db, who, tenant, intent, target, request)
	}
	if err := authority(ctx); err != nil {
		return empty, LockedRemovalPreviewSelection{}, err
	}
	lookup, err := p.resolve(ctx, who, tenant, intent, target, request)
	if err != nil {
		return empty, LockedRemovalPreviewSelection{}, err
	}
	if err := ctx.Err(); err != nil {
		return empty, LockedRemovalPreviewSelection{}, err
	}
	params := lookup.Params
	lookup.SourceRaw = append([]byte(nil), lookup.SourceRaw...)
	if _, _, err := FreezeRemovalPreviewParams(params); err != nil {
		return empty, LockedRemovalPreviewSelection{}, err
	}
	if lookup.RunID == uuid.Nil || params.TenantID != tenant || params.IntentID != intent || params.TargetID != target ||
		params.WorkflowRevision != request.ExpectedRevision || params.TargetRevision != request.TargetRevision ||
		params.Plan.SHA256 != request.PlanSHA || params.Candidate.SHA256 != request.CandidateSHA {
		return empty, LockedRemovalPreviewSelection{}, ErrConflict
	}
	if err := authority(ctx); err != nil {
		return empty, LockedRemovalPreviewSelection{}, err
	}
	physical, err := ReadPhysicalRemovalPreview(ctx, p.db, p.store, p.bucket, who, lookup.RunID, params,
		append([]byte(nil), lookup.SourceRaw...), p.iccLockSHA, p.srgbSHA)
	if err != nil {
		return empty, LockedRemovalPreviewSelection{}, err
	}
	receipt, err := SealRemovalPreviewReceipt(ctx, p.db, who, lookup.RunID, physical)
	if err != nil {
		return empty, LockedRemovalPreviewSelection{}, err
	}
	if err = authority(ctx); err != nil {
		return empty, LockedRemovalPreviewSelection{}, err
	}
	return physical, LockedRemovalPreviewSelection{TargetID: target, PreviewRunID: lookup.RunID, Receipt: receipt}, nil
}

// LockedRemovalPreviewSelection is server-resolved producer linkage. It does
// not assert that the Apply transaction has acquired any locks yet.
type LockedRemovalPreviewSelection struct {
	TargetID     uuid.UUID
	PreviewRunID uuid.UUID
	Receipt      RemovalPreviewReceipt
}

// Recheck the exact verified producer and pinned objects around URL signing.
// A current workflow alone does not authorize an older canonical result.
func checkRemovalIssuedLinkage(expected, current RemovalPreviewPresentationLinkage) error {
    if current.Sidecars != expected.Sidecars || current.Document.Source != expected.Document.Source ||
        current.Document.TaskID != expected.Document.TaskID || current.Document.ResultID != expected.Document.ResultID ||
        current.Document.Attempt != expected.Document.Attempt || current.Document.Fence != expected.Document.Fence {
        return ErrConflict
    }
    return nil
}

func recheckRemovalIssuedPreview(ctx context.Context, db previewOutputQueryer, store planObjectStore,
    bucket string, who auth.Identity, run uuid.UUID, physical PhysicalRemovalPreview,
    iccLockSHA, srgbSHA string) error {
    if !physical.verified { return ErrInvalid }
    current, err := ReadRemovalPreviewPresentationLinkage(ctx, db, store, bucket, who, run,
        physical.linkage.Presentation.Params, iccLockSHA, srgbSHA)
    if err != nil { return err }
    return checkRemovalIssuedLinkage(physical.linkage, current)
}
