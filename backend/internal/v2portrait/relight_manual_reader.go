package v2portrait

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"sort"
)

type SQLRelightManualReader struct {
	DB      DonorConfirmationTransactions
	Objects v2input.SessionAnalysisObjectReader
	Bucket  string
	Limits  RelightManualRasterLimits
	Budget  *RelightManualRasterBudget
}

type RelightManualProducer struct {
	Root, Original                    v2input.Ref
	RootBody, ParamsBody, VectorsBody []byte
	Inputs                            []v2input.Entry
	preintent                         *relightManualPreintentWitness
}

func (s SQLRelightManualReader) lookup(ctx context.Context, tenant, run uuid.UUID, intent RelightIntent, mask, root []byte, protected bool, kind string) (relightProducerLocator, error) {
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
	var rootArg any
	if root != nil {
		rootArg = string(root)
	}
	var raw []byte
	if err = tx.QueryRow(ctx, "SELECT photo.v28_relight_intent_manual_object($1,$2,$3,$4,$5,$6)", tenant, run, string(mask), rootArg, protected, kind).Scan(&raw); err != nil {
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
	media := "application/json"
	maxBytes := int64(1048576)
	minBytes := int64(2)
	if kind == "target" || kind == "guard" {
		media = "application/octet-stream"
		maxBytes = 800065536
		minBytes = 1
	}
	if err != nil || !bytes.Equal(normalized, encoded) || locator.ArtifactID == uuid.Nil || locator.Bucket != s.Bucket || locator.Key == "" || locator.Version == "" || locator.Version == "null" || locator.Bytes < minBytes || locator.Bytes > maxBytes || locator.Media != media {
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

func manualLocatorRef(v relightProducerLocator) v2input.Ref {
	return v2input.Ref{ArtifactID: v.ArtifactID, SHA256: v.SHA256}
}
func manualLocatorObject(v relightProducerLocator) objectstore.Object {
	return objectstore.Object{Key: v.Key, VersionID: v.Version, SHA256: v.SHA256, Bytes: v.Bytes, ContentType: v.Media}
}
func (s SQLRelightManualReader) document(ctx context.Context, v relightProducerLocator) ([]byte, error) {
	stream, err := s.Objects.Open(ctx, manualLocatorObject(v))
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(stream, v.Bytes+1))
	closeErr := stream.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	sum := sha256.Sum256(raw)
	if int64(len(raw)) != v.Bytes || hex.EncodeToString(sum[:]) != v.SHA256 {
		return nil, v2input.ErrInvalid
	}
	normalized, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(normalized, raw) {
		return nil, v2input.ErrInvalid
	}
	return raw, nil
}

// Load verifies both original raster companions and rechecks all pinned object
// locators after IO. It grants no durable admission; caller must share the request budget across retries
// and repeat source/producer authority in its admission transaction.
func (s SQLRelightManualReader) Load(ctx context.Context, tenant, run uuid.UUID, intent RelightIntent, documents RelightInputDocuments, protected bool) (RelightManualProducer, error) {
	var zero RelightManualProducer
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if s.DB == nil || s.Objects == nil || s.Budget == nil || s.Bucket == "" || tenant == uuid.Nil || run == uuid.Nil {
		return zero, v2input.ErrInvalid
	}
	var params RelightParams
	if json.Unmarshal([]byte(intent.ParamsJCS), &params) != nil {
		return zero, v2input.ErrInvalid
	}
	body, sha, err := params.Compile()
	if err != nil || string(body) != intent.ParamsJCS || sha != intent.ParamsSHA {
		return zero, v2input.ErrInvalid
	}
	if _, err = RelightMaskInputs(params, intent.Qualification, documents.Source, documents.Subject, documents.Protected); err != nil {
		return zero, err
	}
	mask := documents.Subject
	if protected {
		mask = documents.Protected
	}
	parsed, err := review.ParseMaskRef(mask)
	if err != nil || parsed.Selector != "manual" {
		return zero, v2input.ErrInvalid
	}
	locators := map[string]relightProducerLocator{}
	var rootBody []byte
	locate := func(kind string) (relightProducerLocator, error) {
		return s.lookup(ctx, tenant, run, intent, mask, rootBody, protected, kind)
	}
	locators["root"], err = locate("root")
	if err != nil {
		return zero, err
	}
	rootBody, err = s.document(ctx, locators["root"])
	if err != nil {
		return zero, err
	}
	rootRef := manualLocatorRef(locators["root"])
	root, err := review.ParseManualRasterDocument(rootBody, review.ArtifactRef{ID: rootRef.ArtifactID, SHA256: rootRef.SHA256})
	if err != nil {
		return zero, err
	}
	locators["params"], err = locate("params")
	if err != nil {
		return zero, err
	}
	paramsBody, err := s.document(ctx, locators["params"])
	if err != nil {
		return zero, err
	}
	original := manualLocatorRef(locators["params"])
	var vectorsBody []byte
	vectorRef := v2input.Ref{ArtifactID: root.Vectors.ID, SHA256: root.Vectors.SHA256}
	if vectorRef != original {
		locators["vectors"], err = locate("vectors")
		if err != nil {
			return zero, err
		}
		if manualLocatorRef(locators["vectors"]) != vectorRef {
			return zero, v2input.ErrInvalid
		}
		vectorsBody, err = s.document(ctx, locators["vectors"])
		if err != nil {
			return zero, err
		}
	}
	plan, err := ResolveRelightManualPlan(ctx, params, documents.Source, mask, paramsBody, rootBody, vectorsBody, original, rootRef, protected)
	if err != nil {
		return zero, err
	}
	if err = s.Budget.reserve(ctx, plan, s.Limits); err != nil {
		return zero, err
	}
	for _, kind := range []string{"target", "guard"} {
		locators[kind], err = locate(kind)
		if err != nil {
			return zero, err
		}
	}
	if manualLocatorRef(locators["target"]) != (v2input.Ref{ArtifactID: root.Target.Artifact.ID, SHA256: root.Target.Artifact.SHA256}) || manualLocatorRef(locators["guard"]) != (v2input.Ref{ArtifactID: root.Protected.Artifact.ID, SHA256: root.Protected.Artifact.SHA256}) {
		return zero, v2input.ErrInvalid
	}
	if err = VerifyRelightManualPixels(ctx, s.Objects, plan, manualLocatorObject(locators["target"]), manualLocatorObject(locators["guard"]), s.Limits); err != nil {
		return zero, err
	}
	inputs := make([]v2input.Entry, 0, len(locators))
	for _, kind := range []string{"root", "params", "vectors", "target", "guard"} {
		before, exists := locators[kind]
		if !exists {
			continue
		}
		after, e := locate(kind)
		if e != nil {
			return zero, e
		}
		if before != after {
			return zero, v2input.ErrExpired
		}
		contract := map[string]string{"root": "RasterizedMask", "params": "Params", "vectors": "ManualMask", "target": "NpyArray", "guard": "NpyArray"}[kind]
		inputs = append(inputs, v2input.Entry{Ref: manualLocatorRef(before), Contract: contract})
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	sort.Slice(inputs, func(i, j int) bool { return inputs[i].Ref.ArtifactID.String() < inputs[j].Ref.ArtifactID.String() })
	return RelightManualProducer{Root: rootRef, Original: original, RootBody: rootBody, ParamsBody: paramsBody, VectorsBody: vectorsBody, Inputs: inputs}, nil
}
