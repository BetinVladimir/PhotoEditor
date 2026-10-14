package v2portrait

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

type RelightDepthProducer struct {
	Ref  v2input.Ref
	Body []byte
}

type SQLRelightDepthReader struct {
	DB      DonorConfirmationTransactions
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
}

type relightProducerLocator struct {
	ArtifactID uuid.UUID `json:"artifact_id"`
	SHA256     string    `json:"sha256"`
	Bucket     string    `json:"bucket"`
	Key        string    `json:"object_key"`
	Version    string    `json:"object_version"`
	Bytes      int64     `json:"bytes"`
	Media      string    `json:"media_type"`
}

func (s SQLRelightDepthReader) lookup(ctx context.Context, tenant, run uuid.UUID, intent RelightIntent, depthRaw []byte, originSQL string) (relightProducerLocator, error) {
	var zero relightProducerLocator
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(context.Background())
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	if err = SaveRelightIntent(ctx, tx, tenant, run, intent); err != nil {
		return zero, err
	}
	var raw []byte
	if err = tx.QueryRow(ctx, originSQL, tenant, run, string(depthRaw)).Scan(&raw); err != nil {
		return zero, err
	}
	var locator relightProducerLocator
	if len(raw) < 2 || len(raw) > 16384 || json.Unmarshal(raw, &locator) != nil {
		return zero, v2input.ErrExpired
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil {
		return zero, err
	}
	encoded, err := canonical.Marshal(locator)
	if err != nil || !bytes.Equal(normalized, encoded) || locator.ArtifactID == uuid.Nil || locator.Bucket != s.Bucket || locator.Key == "" || locator.Version == "" || locator.Version == "null" || locator.Bytes < 2 || locator.Bytes > 65536 || locator.Media != "application/json" {
		return zero, v2input.ErrInvalid
	}
	if _, err = domain.ParseDigest(locator.SHA256); err != nil {
		return zero, v2input.ErrInvalid
	}
	if err = tx.Commit(ctx); err != nil {
		return zero, err
	}
	return locator, nil
}

// Load performs object IO between committed discovery transactions, then
// revalidates the same frozen intent and original locator. Admission must repeat
// authority under its own retained locks; this is not a transferable grant.
func (s SQLRelightDepthReader) Load(ctx context.Context, tenant, run uuid.UUID, intent RelightIntent,
	sourceRaw, depthRaw []byte) (RelightDepthProducer, error) {
	return s.loadProducer(ctx, tenant, run, intent, depthRaw, "SELECT photo.v28_relight_intent_depth_origin($1,$2,$3)", func(p RelightParams) error {
		_, err := RelightDepthInputs(p, intent.Qualification, sourceRaw, depthRaw)
		return err
	}, VerifyRelightDepthProducerParams)
}

func (s SQLRelightDepthReader) loadProducer(ctx context.Context, tenant, run uuid.UUID, intent RelightIntent, depthRaw []byte, originSQL string, validate func(RelightParams) error, verify func(RelightParams, v2input.Ref, []byte) error) (RelightDepthProducer, error) {
	var zero RelightDepthProducer
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil || run == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	var params RelightParams
	if json.Unmarshal([]byte(intent.ParamsJCS), &params) != nil {
		return zero, v2input.ErrInvalid
	}
	body, sha, err := params.Compile()
	if err != nil || !bytes.Equal(body, []byte(intent.ParamsJCS)) || sha != intent.ParamsSHA {
		return zero, v2input.ErrInvalid
	}
	if err = validate(params); err != nil {
		return zero, err
	}
	before, err := s.lookup(ctx, tenant, run, intent, depthRaw, originSQL)
	if err != nil {
		return zero, err
	}
	stream, err := s.Objects.Open(ctx, objectstore.Object{Key: before.Key, VersionID: before.Version, SHA256: before.SHA256, ContentType: before.Media, Bytes: before.Bytes})
	if err != nil {
		return zero, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(stream, before.Bytes+1))
	closeErr := stream.Close()
	if readErr != nil {
		return zero, readErr
	}
	if closeErr != nil {
		return zero, closeErr
	}
	if int64(len(raw)) != before.Bytes {
		return zero, v2input.ErrInvalid
	}
	ref := v2input.Ref{ArtifactID: before.ArtifactID, SHA256: before.SHA256}
	if err = verify(params, ref, raw); err != nil {
		return zero, err
	}
	after, err := s.lookup(ctx, tenant, run, intent, depthRaw, originSQL)
	if err != nil {
		return zero, err
	}
	if after != before {
		return zero, v2input.ErrExpired
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return RelightDepthProducer{Ref: ref, Body: append([]byte(nil), raw...)}, nil
}
