package v2region

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2render"
)

// PreparedAuthoredRegion retains actual full authored bytes, not a projection
// to the scalar FrozenRecipe. Qualification/closure/resource admission and a
// durable owned compound request remain required before any child is queued.
type PreparedAuthoredRegion struct {
	Project              uuid.UUID
	Source               RegionSourceMetadata
	RecipeObject         objectstore.Object
	Binding              v2input.SessionVersionBinding
	Evidence             v2render.CompiledRenderEvidence
	Geometry             RegionGeometry
	RecipeRaw, SourceRaw []byte
}

type SQLAuthoredRegionPreparation struct {
	API     *pgxpool.Pool
	Objects RegionObjectReader
	Bucket  string
}

func (s SQLAuthoredRegionPreparation) currentSource(ctx context.Context, who auth.Identity, tenant, asset uuid.UUID, r AuthoredRegionRequest) (RegionSourceMetadata, error) {
	var source RegionSourceMetadata
	err := database.TenantTransaction(ctx, s.API, tenant, func(tx pgx.Tx) error {
		var role string
		if err := tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
			return err
		}
		if role != "photo_api" {
			return ErrRegionInvalid
		}
		var err error
		source, err = ReadRegionSource(ctx, tx, s.Bucket, tenant, asset, r.Preview.VersionID, who.UserID, who.SessionID)
		if err != nil {
			return err
		}
		var live bool
		err = tx.QueryRow(ctx, `SELECT photo.authored_region_current_request($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, tenant, asset, r.FrozenDocument, r.DraftDocument, r.DraftRevision, r.Preview.Recipe.ArtifactID, r.Preview.Recipe.SHA256, r.Preview.VersionID, source.Ref.ArtifactID, source.Ref.SHA256, who.UserID, who.SessionID).Scan(&live)
		if err != nil {
			return err
		}
		if !live {
			return ErrRegionInvalid
		}
		return nil
	})
	return source, err
}

func (s SQLAuthoredRegionPreparation) Prepare(ctx context.Context, who auth.Identity, tenant, asset uuid.UUID, r AuthoredRegionRequest) (PreparedAuthoredRegion, error) {
	var out PreparedAuthoredRegion
	if err := ctx.Err(); err != nil {
		return out, err
	}
	if s.API == nil || s.Objects == nil || s.Bucket == "" || tenant == uuid.Nil || asset == uuid.Nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil {
		return out, ErrRegionInvalid
	}
	// Decode the public shape again even for internal callers.
	raw, err := authoredRegionRequestBytes(r)
	if err != nil {
		return out, err
	}
	r, err = DecodeAuthoredRegionRequest(raw)
	if err != nil {
		return out, err
	}
	source, err := s.currentSource(ctx, who, tenant, asset, r)
	if err != nil {
		return out, err
	}
	recipeRef := v2input.Ref{ArtifactID: r.Preview.Recipe.ArtifactID, SHA256: r.Preview.Recipe.SHA256}
	catalog := v2draft.SQLArtifactCatalog{DB: s.API, Bucket: s.Bucket, ProjectID: source.ProjectID, AssetVersionID: r.Preview.VersionID}
	node := v2draft.LayerRef{Ref: recipeRef, Contract: "FrozenRenderRecipe"}
	recipeObject, err := catalog.Lookup(ctx, who, tenant, node)
	if err != nil {
		return out, err
	}
	// No open database transaction or lock spans either immutable object read.
	recipeRaw, err := readAuthoredRegionDocument(ctx, s.Objects, recipeObject, recipeRef)
	if err != nil {
		return out, err
	}
	sourceRaw, err := readAuthoredRegionDocument(ctx, s.Objects, source.Object, source.Ref)
	if err != nil {
		return out, err
	}
	frozen, err := v2draft.DecodeFrozenRenderRecipe(recipeRaw)
	if err != nil {
		return out, err
	}
	binding := frozen.Scalars.Source
	if binding.AssetVersionID != r.Preview.VersionID || binding.Source != source.Ref {
		return out, ErrRegionInvalid
	}
	evidence, err := v2render.CompileRenderEvidence(binding, recipeRef, recipeRaw, sourceRaw)
	if err != nil {
		return out, err
	}
	geometry, err := CompileRegionGeometry(int64(evidence.Source.Canvas.Width), int64(evidence.Source.Canvas.Height), r.Preview.Rect, r.Preview.MaxEdge)
	if err != nil {
		return out, err
	}
	currentObject, err := catalog.Lookup(ctx, who, tenant, node)
	if err != nil {
		return out, err
	}
	currentSource, err := s.currentSource(ctx, who, tenant, asset, r)
	if err != nil {
		return out, err
	}
	if currentObject != recipeObject || currentSource != source {
		return out, ErrRegionInvalid
	}
	if err = ctx.Err(); err != nil {
		return out, err
	}
	return PreparedAuthoredRegion{Project: source.ProjectID, Source: source, RecipeObject: recipeObject, Binding: binding, Evidence: evidence, Geometry: geometry, RecipeRaw: append([]byte(nil), recipeRaw...), SourceRaw: append([]byte(nil), sourceRaw...)}, nil
}

func readAuthoredRegionDocument(ctx context.Context, objects RegionObjectReader, object objectstore.Object, ref v2input.Ref) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if objects == nil || ref.ArtifactID == uuid.Nil || !validRegionSHA(ref.SHA256) || object.SHA256 != ref.SHA256 || object.ContentType != "application/json" || object.Bytes < 2 || object.Bytes > 1048576 || object.Key == "" || object.VersionID == "" || object.VersionID == "null" {
		return nil, ErrRegionInvalid
	}
	stream, err := objects.Open(ctx, object)
	if err != nil {
		if stream != nil {
			_ = stream.Close()
		}
		return nil, err
	}
	if stream == nil {
		return nil, ErrRegionInvalid
	}
	body, readErr := io.ReadAll(io.LimitReader(checkedRegionReader{ctx, stream}, object.Bytes+1))
	closeErr := stream.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(body)
	if int64(len(body)) != object.Bytes || hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, ErrRegionInvalid
	}
	return body, nil
}
