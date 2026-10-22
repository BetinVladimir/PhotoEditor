package semanticreview

import (
	"context"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"time"
)

// ExposurePreviewLookup is server-resolved state, never a client-supplied run or
// source document. Resolution may resume a separately admitted proposal render.
type ExposurePreviewLookup struct {
	RunID     uuid.UUID
	Params    ExposurePreviewParams
	SourceRaw []byte
}

type ExposurePreviewResolver func(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, PreviewRequest) (ExposurePreviewLookup, error)

type ExposurePreviewProvider struct {
	db                  *pgxpool.Pool
	store               planObjectStore
	bucket              string
	resolve             ExposurePreviewResolver
	sign                PreviewURLSigner
	iccLockSHA, srgbSHA string
}

func NewExposurePreviewProvider(db *pgxpool.Pool, store planObjectStore, bucket string,
	resolve ExposurePreviewResolver, sign PreviewURLSigner, iccLockSHA, srgbSHA string) (*ExposurePreviewProvider, error) {
	if db == nil || store == nil || bucket == "" || resolve == nil || sign == nil ||
		!shaPattern.MatchString(iccLockSHA) || !shaPattern.MatchString(srgbSHA) {
		return nil, ErrInvalid
	}
	return &ExposurePreviewProvider{db: db, store: store, bucket: bucket, resolve: resolve, sign: sign,
		iccLockSHA: iccLockSHA, srgbSHA: srgbSHA}, nil
}

// ReadPreview implements the HTTP provider contract without borrowing Apply
// authority. All resolver output is re-bound to the authenticated request.
func (p *ExposurePreviewProvider) ReadPreview(ctx context.Context, who auth.Identity,
	tenant, intent, target uuid.UUID, request PreviewRequest) (Preview, error) {
	physical, _, err := p.ReadPhysicalPreview(ctx, who, tenant, intent, target, request)
	if err != nil {
		return Preview{}, err
	}
	authority := func(ctx context.Context) error {
		return CheckCurrentPreviewAuthority(ctx, p.db, who, tenant, intent, target, request)
	}
	return BuildExposurePreviewResponse(ctx, physical, request, time.Now(), p.sign, authority)
}

// ReadPhysicalPreview repeats physical validation and seals canonical receipt
// without issuing download URLs. Apply must independently lock the selection.
func (p *ExposurePreviewProvider) ReadPhysicalPreview(ctx context.Context, who auth.Identity,
	tenant, intent, target uuid.UUID, request PreviewRequest) (PhysicalExposurePreview, LockedPreviewSelection, error) {
	empty := PhysicalExposurePreview{}
	if err := ctx.Err(); err != nil {
		return empty, LockedPreviewSelection{}, err
	}
	if p == nil || p.db == nil || p.resolve == nil || p.sign == nil || p.store == nil {
		return empty, LockedPreviewSelection{}, ErrUnavailable
	}
	authority := func(ctx context.Context) error {
		return CheckCurrentPreviewAuthority(ctx, p.db, who, tenant, intent, target, request)
	}
	if err := authority(ctx); err != nil {
		return empty, LockedPreviewSelection{}, err
	}
	lookup, err := p.resolve(ctx, who, tenant, intent, target, request)
	if err != nil {
		return empty, LockedPreviewSelection{}, err
	}
	if err := ctx.Err(); err != nil {
		return empty, LockedPreviewSelection{}, err
	}
	params := lookup.Params
	if lookup.RunID == uuid.Nil || params.TenantID != tenant || params.IntentID != intent || params.TargetID != target ||
		params.WorkflowRevision != request.ExpectedRevision || params.TargetRevision != request.TargetRevision ||
		params.Plan.SHA256 != request.PlanSHA || params.Candidate.SHA256 != request.CandidateSHA {
		return empty, LockedPreviewSelection{}, ErrConflict
	}
	if err := authority(ctx); err != nil {
		return empty, LockedPreviewSelection{}, err
	}
	physical, err := ReadPhysicalExposurePreview(ctx, p.db, p.store, p.bucket, who, lookup.RunID, params,
		append([]byte(nil), lookup.SourceRaw...), p.iccLockSHA, p.srgbSHA)
	if err != nil {
		return empty, LockedPreviewSelection{}, err
	}
	receipt, err := SealExposurePreviewReceipt(ctx, p.db, who, lookup.RunID, physical)
	if err != nil {
		return empty, LockedPreviewSelection{}, err
	}
	if err = authority(ctx); err != nil {
		return empty, LockedPreviewSelection{}, err
	}
	return physical, LockedPreviewSelection{TargetID: target, PreviewRunID: lookup.RunID, Receipt: receipt}, nil
}
