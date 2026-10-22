package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2resource"
)

type ReplaceRequest struct {
 EditorBridge *EditorRecipeBridgeRequest `json:"editor_bridge,omitempty"`
	ExpectedRevision string          `json:"expected_revision"`
	Recipe           json.RawMessage `json:"recipe"`
	Layers           json.RawMessage `json:"layers"`
}

func replaceRevision(value string) (int64, error) {
	if value == "0" {
		return 0, nil
	}
	if value == "" || len(value) > 19 || value[0] == '0' {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return 0, domain.NewError("VALIDATION_FAILED", 422)
		}
	}
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil || n < 1 {
		return 0, domain.NewError("VALIDATION_FAILED", 422)
	}
	return n, nil
}

func stableDraftID(tenantID, actorID, assetID uuid.UUID, key, kind string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("v2-draft/"+tenantID.String()+"/"+
		actorID.String()+"/"+assetID.String()+"/"+key+"/"+kind))
}

func draftMutationError(err error) error {
	if errors.Is(err, v2input.ErrInvalid) {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	if errors.Is(err, objectstore.ErrUnavailable) || errors.Is(err, objectstore.ErrMissing) ||
		errors.Is(err, objectstore.ErrIntegrity) {
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var pg *pgconn.PgError
	if !errors.As(err, &pg) {
		return err
	}
	if pg.Message == "V24_IDENTITY_CONSENT_REVOKED" {
		return domain.NewError("CONSENT_REVOKED", 409)
	}
	if pg.Message == "V23_PROFILE_CONSENT_REVOKED" {
		return domain.NewError("PROFILE_CONSENT_REVOKED", 409)
	}
	switch pg.Code {
	case "40001", "23505", "23514":
		return domain.NewError("REVISION_CONFLICT", 409)
	case "42501":
		return domain.NewError("FORBIDDEN", 403)
	case "22023":
		return domain.NewError("VALIDATION_FAILED", 422)
	case "42P01", "42883":
		return domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	return err
}

// Replace stages immutable documents before one fenced, idempotent CAS. A
// rejected CAS leaves only temporary uploads, which the V21 janitor retires.
func (s Service) Replace(ctx context.Context, who auth.Identity, tenantID,
	assetID uuid.UUID, key string, req ReplaceRequest) (idempotency.Response, error) {
	if s.DB == nil || s.Objects == nil || s.Bucket == "" {
		return idempotency.Response{}, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	if assetID == uuid.Nil {
		return idempotency.Response{}, domain.NewError("NOT_FOUND", 404)
	}
	if !idempotency.ValidKey(key) {
		return idempotency.Response{}, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	// Freeze caller-owned raw messages before receipt waits or object IO.
	req.Recipe=append(json.RawMessage(nil),req.Recipe...)
	req.Layers=append(json.RawMessage(nil),req.Layers...)
	if req.EditorBridge!=nil { bridge:=*req.EditorBridge;bridge.Params=append(json.RawMessage(nil),bridge.Params...);req.EditorBridge=&bridge }
	expected, err := replaceRevision(req.ExpectedRevision)
	if err != nil {
		return idempotency.Response{}, err
	}
	if len(req.Recipe) == 0 || len(req.Layers) == 0 {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	if replay,found,e:=s.replayReplacement(ctx,who,tenantID,assetID,key,req);e!=nil||found{return replay,e}
	binding, projectID, image, err := (BindingResolver{DB: s.DB, Objects: s.Objects,
		Bucket: s.Bucket}).CurrentScopedImage(ctx, who, tenantID, assetID)
	if err != nil {
		return idempotency.Response{}, draftMutationError(err)
	}

	bridgeRecipe, bridgeRef, err := s.prepareEditorRecipeBridge(ctx,who,tenantID,assetID,key,expected,req,binding,image.Canvas.SourceSHA256)
	if err != nil { return idempotency.Response{},draftMutationError(err) }
	publisher := Publisher{DB: s.DB, Objects: s.Objects, Bucket: s.Bucket}
	catalog := SQLArtifactCatalog{DB: s.DB, Bucket: s.Bucket,
		ProjectID: projectID, AssetVersionID: binding.AssetVersionID}
	resolver := ClosureResolver{Catalog: catalog, Objects: s.Objects}
	var layerID, baseID uuid.UUID
	var layerRef *v2input.Ref
	var layerRefs []DocumentRef
	if !bytes.Equal(bytes.TrimSpace(req.Layers), []byte("null")) {
		layerID = stableDraftID(tenantID, who.UserID, assetID, key, "layer")
		body, e := canonical.Bytes(req.Layers)
		if e != nil || len(body) > 1048576 {
			return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
		}
		sum := sha256.Sum256(body)
		provisional := v2input.Ref{ArtifactID: layerID, SHA256: hex.EncodeToString(sum[:])}
		if _, e = PrepareDraft(bridgeRecipe, req.Layers, binding, &provisional, expected == 0); e != nil {
			return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
		}
		published, e := publisher.Publish(ctx, who, tenantID, binding.AssetVersionID,
			layerID, body)
		if e != nil {
			return idempotency.Response{}, draftMutationError(e)
		}
		layerRef = &published
		layerRefs, e = resolver.ResolveLayer(ctx, who, tenantID, published, binding)
		if e != nil {
			return idempotency.Response{}, draftMutationError(e)
		}
	}
	prepared, err := PrepareDraft(bridgeRecipe, req.Layers, binding, layerRef, expected == 0)
	if err != nil {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	var learningDependency struct {
		VersionID *uuid.UUID `json:"learning_version_id"`
	}
	if err = json.Unmarshal(prepared.Recipe, &learningDependency); err != nil {
		return idempotency.Response{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	refs, err := resolver.Resolve(ctx, who, tenantID, prepared.Recipe, binding)
	if err != nil {
		return idempotency.Response{}, draftMutationError(err)
	}
	baseRefs := []DocumentRef{}
	var baseRef v2input.Ref
	if expected == 0 {
		baseRefs, err = resolver.Resolve(ctx, who, tenantID, prepared.Base, binding)
		if err != nil {
			return idempotency.Response{}, draftMutationError(err)
		}
		baseID = stableDraftID(tenantID, who.UserID, assetID, key, "base")
		baseRef, err = publisher.Publish(ctx, who, tenantID, binding.AssetVersionID,
			baseID, prepared.Base)
		if err != nil {
			return idempotency.Response{}, draftMutationError(err)
		}
	}
	documentID := stableDraftID(tenantID, who.UserID, assetID, key, "recipe")
	ref, err := publisher.Publish(ctx, who, tenantID, binding.AssetVersionID,
		documentID, prepared.Recipe)
	if err != nil {
		return idempotency.Response{}, draftMutationError(err)
	}
	refsJSON, _ := json.Marshal(refs)
	baseRefsJSON, _ := json.Marshal(baseRefs)
	var layerRefsJSON []byte
	var layerArtifact, layerSHA any
	if layerRef != nil {
		layerRefsJSON, _ = json.Marshal(layerRefs)
		layerArtifact, layerSHA = layerRef.ArtifactID, layerRef.SHA256
	}
	var baseDocument, baseArtifact, baseSHA any
	if expected == 0 {
		baseDocument, baseArtifact, baseSHA = baseID, baseRef.ArtifactID, baseRef.SHA256
	}
	var out idempotency.Response
	err = tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, _ domain.Role) error {
			var e error
			out, e = idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tenantID,
				Principal: who.UserID, PrincipalType: "user",
				Route: "Draft.Replace." + assetID.String(), Key: key},
				req, time.Now().Add(24*time.Hour), func() (idempotency.Response, error) {
					if req.EditorBridge!=nil { if e:=checkEditorBridge(ctx,tx,who,tenantID,binding.AssetVersionID,req.EditorBridge);e!=nil{return idempotency.Response{},e} }
					var result json.RawMessage
					e := tx.QueryRow(ctx, `SELECT photo.replace_v23_draft(
					 $1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`,
						tenantID, binding.AssetVersionID, expected, documentID,
						ref.ArtifactID, ref.SHA256, who.UserID, who.SessionID,
						refsJSON, baseDocument, baseArtifact, baseSHA, baseRefsJSON,
						nullUUID(layerID), layerArtifact, layerSHA, layerRefsJSON,
						learningDependency.VersionID).Scan(&result)
					if e != nil {
						return idempotency.Response{}, e
					}
                    if bridgeRef!=nil {
                     revision,_:=replaceRevision(req.EditorBridge.Revision)
                     if _,e=tx.Exec(ctx,`INSERT INTO photo.editor_recipe_bridges(tenant_id,document_id,asset_version_id,actor_id,editor_revision,override_artifact_id,override_sha) VALUES($1,$2,$3,$4,$5,$6,$7)`,tenantID,documentID,binding.AssetVersionID,who.UserID,revision,bridgeRef.ArtifactID,bridgeRef.SHA256);e!=nil{return idempotency.Response{},e}
                    }
					id, e := v2resource.ID(result)
					if e != nil {
						return idempotency.Response{}, e
					}
					body, e := v2resource.Load(ctx, tx, tenantID, id, "v2_document_streams")
					return idempotency.Response{Status: 200, Body: body, ResourceID: &id}, e
				})
			return e
		})
	return out, draftMutationError(err)
}
