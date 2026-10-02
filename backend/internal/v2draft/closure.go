package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/editor"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
)

const maxDocumentClosure = 512
const maxRenderDocumentClosure = 4096
const maxDocumentJSON = 8 << 20

type ArtifactCatalog interface {
	Lookup(context.Context, auth.Identity, uuid.UUID, LayerRef) (objectstore.Object, error)
}

type SQLArtifactCatalog struct {
	DB             *pgxpool.Pool
	Bucket         string
	ProjectID      uuid.UUID
	AssetVersionID uuid.UUID
}

// Lookup proves both ready bytes and project/version provenance for the exact
// declared contract. The SQL function repeats active editor authorization.
func (s SQLArtifactCatalog) Lookup(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, node LayerRef) (objectstore.Object, error) {
	var object objectstore.Object
	if s.DB == nil || s.Bucket == "" || s.ProjectID == uuid.Nil ||
		s.AssetVersionID == uuid.Nil {
		return object, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	var bucket string
	scopeSQL := "photo.v2_draft_artifact_scope($1,$4,$5,a.id,a.sha256,$6,$7,$8,$9,$10)"
	if node.Contract == "FrozenRenderRecipe" {
		if node.ParentRef != (v2input.Ref{}) {
			return object, v2input.ErrInvalid
		}
		// Preserve the same argument shape without promoting child refs to roots.
		scopeSQL = "photo.v26_render_recipe_scope($1,$4,$5,a.id,a.sha256,$7,$8) AND $6::text='FrozenRenderRecipe' AND $9::uuid IS NULL AND $10::text IS NULL"
	}
	args := []any{tenantID, node.Ref.ArtifactID, node.Ref.SHA256, s.ProjectID, s.AssetVersionID, node.Contract, who.UserID, who.SessionID, nullUUID(node.ParentRef.ArtifactID), nullString(node.ParentRef.SHA256)}
	if node.Contract == "CandidateProfile" {
		if node.ExpectedLearning == nil || node.ExpectedBinding == nil || node.ExpectedBinding.AssetVersionID != s.AssetVersionID || node.ExpectedLearning.Model != node.Ref || !isDocumentRef(node.ParentRef) || node.ExpectedLearning.VersionID == uuid.Nil {
			return object, v2input.ErrInvalid
		}
		scopeSQL = "photo.v26_render_model_scope($1,$4,$5,$9,$10,a.id,a.sha256,$11,$7,$8) AND $6::text='CandidateProfile'"
		args = append(args, node.ExpectedLearning.VersionID)
	}
	if node.Contract == "DescriptorShard" {
		if node.ExpectedLearning == nil || node.ExpectedBinding == nil || node.ExpectedBinding.AssetVersionID != s.AssetVersionID || node.ExpectedLearning.Descriptor != node.Ref || !isDocumentRef(node.ParentRef) || node.ExpectedLearning.AnalysisID == uuid.Nil || node.ExpectedLearning.Ordinal < 0 || node.ExpectedLearning.Ordinal > 4999 || !correctionDigest(node.ExpectedModelLockSHA) {
			return object, v2input.ErrInvalid
		}
		scopeSQL = "photo.v26_render_descriptor_scope($1,$4,$5,$9,$10,a.id,a.sha256,$11,$12,$13,$7,$8) AND $6::text='DescriptorShard'"
		args = append(args, node.ExpectedLearning.AnalysisID, node.ExpectedLearning.Ordinal, node.ExpectedModelLockSHA)
	}
	if node.Contract == "WorkingImage" && node.RenderFrame != nil {
		frame := node.RenderFrame
		if node.ExpectedBinding == nil || node.ExpectedBinding.Source != node.Ref || frame.SelectedVersion != s.AssetVersionID || !isDocumentRef(frame.Root) || !isDocumentRef(frame.Descriptor) || frame.Learning.Descriptor != frame.Descriptor || frame.Learning.AnalysisID == uuid.Nil || !correctionDigest(frame.ModelLockSHA) || !isDocumentRef(node.ParentRef) {
			return object, v2input.ErrInvalid
		}
		args[4] = node.ExpectedBinding.AssetVersionID
		scopeSQL = "photo.v26_render_frame_source_scope($1,$4,$11,$12,$13,$14,$15,$16,$17,$18,$19,$5,a.id,a.sha256,$7,$8) AND $6::text='WorkingImage' AND $9::uuid IS NOT NULL AND $10::text IS NOT NULL"
		args = append(args, frame.SelectedVersion, frame.Root.ArtifactID, frame.Root.SHA256, frame.Descriptor.ArtifactID, frame.Descriptor.SHA256, frame.Learning.AnalysisID, frame.Learning.Ordinal, frame.ModelLockSHA, frame.Ordinal)
	}
	if node.Contract == "FaceObservations" && node.RenderFrame != nil {
		frame := node.RenderFrame
		if node.ExpectedBinding == nil || frame.SelectedVersion != s.AssetVersionID || !isDocumentRef(frame.Root) || !isDocumentRef(frame.Descriptor) || frame.Learning.Descriptor != frame.Descriptor || frame.Learning.AnalysisID == uuid.Nil || !correctionDigest(frame.ModelLockSHA) || node.ParentRef != frame.Descriptor {
			return object, v2input.ErrInvalid
		}
		args[4] = node.ExpectedBinding.AssetVersionID
		scopeSQL = "photo.v26_render_frame_faces_scope($1,$4,$11,$12,$13,$14,$15,$16,$17,$18,$19,$5,$20,$21,a.id,a.sha256,$7,$8) AND $6::text='FaceObservations' AND $9::uuid=$14 AND $10::text=$15"
		args = append(args, frame.SelectedVersion, frame.Root.ArtifactID, frame.Root.SHA256, frame.Descriptor.ArtifactID, frame.Descriptor.SHA256, frame.Learning.AnalysisID, frame.Learning.Ordinal, frame.ModelLockSHA, frame.Ordinal, node.ExpectedBinding.Source.ArtifactID, node.ExpectedBinding.Source.SHA256)
	}
	if (node.Contract == "WorkingPixels" || node.Contract == "WorkingAlpha") && node.RenderFrame != nil {
		frame := node.RenderFrame
		if node.ExpectedBinding == nil || node.ParentRef != node.ExpectedBinding.Source || frame.SelectedVersion != s.AssetVersionID || !isDocumentRef(frame.Root) || !isDocumentRef(frame.Descriptor) || frame.Learning.Descriptor != frame.Descriptor || frame.Learning.AnalysisID == uuid.Nil || !correctionDigest(frame.ModelLockSHA) {
			return object, v2input.ErrInvalid
		}
		args[4] = node.ExpectedBinding.AssetVersionID
		scopeSQL = "photo.v26_render_frame_array_scope($1,$4,$11,$12,$13,$14,$15,$16,$17,$18,$19,$5,$20,$21,a.id,a.sha256,$6,$7,$8) AND $9::uuid=$20 AND $10::text=$21"
		args = append(args, frame.SelectedVersion, frame.Root.ArtifactID, frame.Root.SHA256, frame.Descriptor.ArtifactID, frame.Descriptor.SHA256, frame.Learning.AnalysisID, frame.Learning.Ordinal, frame.ModelLockSHA, frame.Ordinal, node.ExpectedBinding.Source.ArtifactID, node.ExpectedBinding.Source.SHA256)
	}
	err := tenant.WithTenant(ctx, s.DB, who, tenantID, domain.Editor,
		func(tx pgx.Tx, _ domain.Role) error {
			err := tx.QueryRow(ctx, `SELECT a.bytes,a.bucket,a.object_key,
			 a.object_version,a.media_type,a.sha256 FROM photo.artifacts a
			 WHERE a.tenant_id=$1 AND a.id=$2 AND a.sha256=$3 AND a.state='ready'
			  AND a.verified_at IS NOT NULL AND a.object_version IS NOT NULL
			  AND a.object_version NOT IN('','null')
			  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
			  AND `+scopeSQL+`
             AND ($6<>'WorkingImage' OR EXISTS(SELECT 1 FROM photo.asset_versions v
              JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
              WHERE v.tenant_id=$1 AND v.id=$5 AND asset.project_id=$4
               AND asset.deleted_at IS NULL AND asset.upload_state='verified'
               AND v.review_state='accepted' AND v.master_artifact_id=a.id AND v.content_hash=a.sha256))`,
				args...).Scan(&object.Bytes, &bucket,
				&object.Key, &object.VersionID, &object.ContentType, &object.SHA256)
			if errors.Is(err, pgx.ErrNoRows) {
				return v2input.ErrInvalid
			}
			return err
		})
	if err != nil {
		return objectstore.Object{}, err
	}
	if bucket != s.Bucket || object.Bytes < 1 {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	return object, nil
}

func nullUUID(id uuid.UUID) any {
	if id == uuid.Nil {
		return nil
	}
	return id
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

type ClosureResolver struct {
	Catalog ArtifactCatalog
	Objects v2input.SessionAnalysisObjectReader
}

func isDocumentRef(ref v2input.Ref) bool {
	if ref.ArtifactID == uuid.Nil || len(ref.SHA256) != 64 {
		return false
	}
	for _, c := range ref.SHA256 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}

func readJSONArtifact(ctx context.Context, objects v2input.SessionAnalysisObjectReader,
	obj objectstore.Object, ref v2input.Ref) ([]byte, error) {
	if obj.ContentType != "application/json" || obj.Bytes < 2 || obj.Bytes > maxDocumentJSON ||
		obj.SHA256 != ref.SHA256 || obj.VersionID == "" || obj.VersionID == "null" {
		return nil, v2input.ErrInvalid
	}
	reader, err := objects.Open(ctx, obj)
	if err != nil {
		return nil, err
	}
	raw, readErr := io.ReadAll(io.LimitReader(reader, maxDocumentJSON+1))
	closeErr := reader.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil || ctx.Err() != nil || int64(len(raw)) != obj.Bytes {
		return nil, v2input.ErrInvalid
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != ref.SHA256 {
		return nil, v2input.ErrInvalid
	}
	canonicalBody, err := canonical.Bytes(raw)
	if err != nil || !bytes.Equal(canonicalBody, raw) {
		return nil, v2input.ErrInvalid
	}
	return raw, nil
}

// nestedRefs discovers exact ArtifactRef objects in canonical JSON. Typed
// contract validation is performed by the caller for known document roots.
func nestedRefs(raw []byte, path, parentContract string, parent v2input.Ref,
	depth int, out *[]LayerRef) error {
	if depth > 32 || len(*out) > maxDocumentClosure {
		return v2input.ErrInvalid
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err == nil && fields != nil {
		if len(fields) == 2 {
			if _, present := fields["artifact_id"]; present {
				ref, ok := layerRef(raw)
				if !ok {
					return v2input.ErrInvalid
				}
				contract := "Artifact"
				switch {
				case parentContract == "WorkingImage" && strings.HasSuffix(path, ".pixels"):
					contract = "WorkingPixels"
				case parentContract == "WorkingImage" && strings.HasSuffix(path, ".alpha"):
					contract = "WorkingAlpha"
				case parentContract == "MaskRef" && strings.HasSuffix(path, ".artifact"):
					contract = "MaskPixels"
				case strings.HasSuffix(path, ".inverse_field"):
					contract = "DenseWarpField"
				}
				*out = append(*out, LayerRef{Path: path, Contract: contract,
					Ref: ref, ParentRef: parent})
				return nil
			}
		}
		keys := make([]string, 0, len(fields))
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := nestedRefs(fields[key], path+"."+key, parentContract, parent, depth+1, out); err != nil {
				return err
			}
		}
		return nil
	}
	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err == nil && items != nil {
		for i, item := range items {
			if err := nestedRefs(item, path+"["+strconv.Itoa(i)+"]", parentContract, parent, depth+1, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// Resolve verifies every reachable artifact before its immutable document is
// attached to a draft stream. The returned slice matches the SQL wire shape.
func (s ClosureResolver) Resolve(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, recipe []byte, binding v2input.SessionVersionBinding) ([]DocumentRef, error) {
	roots, err := recipeRoots(recipe, binding)
	if err != nil {
		return nil, err
	}
	return s.resolveRoots(ctx, who, tenantID, roots, binding)
}

// ResolveLayer returns only a LayerStack document's dependencies. Its own
// artifact is held by v2_documents, so including it in refs is unnecessary.
func (s ClosureResolver) ResolveLayer(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, layer v2input.Ref, binding v2input.SessionVersionBinding) ([]DocumentRef, error) {
	all, err := s.resolveRoots(ctx, who, tenantID, []LayerRef{{Path: "LayerStack",
		Contract: "LayerStack", Ref: layer}}, binding)
	if err != nil {
		return nil, err
	}
	refs := all[1:]
	for i := range refs {
		refs[i].Ordinal = i
	}
	return refs, nil
}

func (s ClosureResolver) resolveRoots(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, roots []LayerRef, binding v2input.SessionVersionBinding) ([]DocumentRef, error) {
	return s.resolveRootsWithInventory(ctx, who, tenantID, roots, binding, nil)
}

func (s ClosureResolver) resolveRootsWithInventory(ctx context.Context, who auth.Identity,
	tenantID uuid.UUID, roots []LayerRef, binding v2input.SessionVersionBinding, inventory *[]v2input.Entry) ([]DocumentRef, error) {
	if s.Catalog == nil || s.Objects == nil || tenantID == uuid.Nil {
		return nil, domain.NewError("DEPENDENCY_UNAVAILABLE", 503)
	}
	closureLimit := maxDocumentClosure
	for _, root := range roots {
		if root.Contract == "FrozenRenderRecipe" {
			closureLimit = maxRenderDocumentClosure
		}
	}
	refs := make([]DocumentRef, 0, len(roots)+4)
	seen := make(map[uuid.UUID]string)
	contracts := make(map[uuid.UUID]string)
	active := make(map[uuid.UUID]bool)
	validatedContracts := make(map[uuid.UUID]string)
	var sourceSHA string
	var sourceTransform []byte
	var walk func(LayerRef, int) error
	walk = func(node LayerRef, depth int) error {
		if depth > 32 || len(refs) >= closureLimit || !isDocumentRef(node.Ref) ||
			active[node.Ref.ArtifactID] {
			return v2input.ErrInvalid
		}
		if prior, ok := seen[node.Ref.ArtifactID]; ok && prior != node.Ref.SHA256 {
			return v2input.ErrInvalid
		}
		if prior := contracts[node.Ref.ArtifactID]; prior != "" &&
			prior != "Artifact" && node.Contract != "Artifact" && prior != node.Contract {
			return v2input.ErrInvalid
		}
		if node.Contract != "Artifact" || contracts[node.Ref.ArtifactID] == "" {
			contracts[node.Ref.ArtifactID] = node.Contract
		}
		refs = append(refs, documentRef(node, len(refs)))
		// Scope is contract- and parent-dependent. A repeated artifact must
		// prove authorization for this occurrence even when its bytes were
		// already validated through another path.
		obj, err := s.Catalog.Lookup(ctx, who, tenantID, node)
		if err != nil {
			return err
		}
		binaryContract := node.Contract == "WorkingPixels" || node.Contract == "WorkingAlpha" ||
			node.Contract == "MaskPixels" || node.Contract == "DenseWarpField"
		if binaryContract && obj.ContentType != "application/octet-stream" && obj.ContentType != "application/x-npy" || !binaryContract && node.Contract != "Artifact" && obj.ContentType != "application/json" {
			return v2input.ErrInvalid
		}
		if inventory != nil {
			contract := node.Contract
			if binaryContract {
				contract = "NpyArray"
			}
			*inventory = append(*inventory, v2input.Entry{Ref: node.Ref, Contract: contract})
		}
		_, repeated := seen[node.Ref.ArtifactID]
		if obj.Bytes < 1 || obj.SHA256 != node.Ref.SHA256 || obj.VersionID == "" || obj.VersionID == "null" {
			return v2input.ErrInvalid
		}
		sameValidation := validatedContracts[node.Ref.ArtifactID] == node.Contract
		if repeated && (node.Contract == "Artifact" || sameValidation && node.Contract != "WorkingImage" && node.Contract != "MaskRef" && node.Contract != "FaceObservations" && node.Contract != "DescriptorShard") {
			return nil
		}
		seen[node.Ref.ArtifactID] = node.Ref.SHA256
		active[node.Ref.ArtifactID] = true
		defer delete(active, node.Ref.ArtifactID)
		if obj.ContentType != "application/json" {
			if !(node.Contract == "Artifact" || binaryContract) || obj.Bytes < 1 ||
				obj.SHA256 != node.Ref.SHA256 || obj.VersionID == "" || obj.VersionID == "null" {
				return v2input.ErrInvalid
			}
			validatedContracts[node.Ref.ArtifactID] = node.Contract
			return nil
		}
		body, err := readJSONArtifact(ctx, s.Objects, obj, node.Ref)
		if err != nil {
			return err
		}
		var children []LayerRef
		compositionParams := false
		switch node.Contract {
		case "FaceObservations":
			if err = validateFaceOccurrence(body, node); err != nil {
				return err
			}
			children, err = faceObservationRefs(body, node.Path, node.Ref)
			if err != nil {
				return err
			}
		case "DescriptorShard":
			children, err = descriptorShardRefs(body, node.Path, node.Ref)
			if err != nil {
				return err
			}
			if err = validateRenderDescriptorOccurrence(body, node); err != nil {
				return err
			}
			if err = bindRenderDescriptorFrames(body, node, children); err != nil {
				return err
			}
		case "CandidateProfile":
			if _, err = DecodeCandidateProfile(body); err != nil {
				return err
			}
		case "FrozenRenderRecipe":
			frozen, decodeErr := DecodeFrozenRenderRecipe(body)
			if decodeErr != nil || frozen.Scalars.Source != binding {
				return v2input.ErrInvalid
			}
			children, err = renderRecipeRefs(body, node.Path, node.Ref)
			if err != nil {
				return err
			}
		case "Params":
			children, compositionParams, err = compositionParamRefs(body, node.Path, node.Ref)
			if err != nil {
				return err
			}
		case "IdentityReport":
			children, err = identityReportRefs(body, node.Path, node.Ref)
			if err != nil {
				return err
			}
		case "ImageReport":
			children, err = imageReportRefs(body, node.Path, node.Ref)
			if err != nil {
				return err
			}
		case "ImageResult":
			children, err = imageResultRefs(body, node.Path, node.Ref)
			if err != nil {
				return err
			}
		case "WorkingImage":
			image, e := review.ParseWorkingImage(body)
			if e != nil {
				return v2input.ErrInvalid
			}
			canvasHash, e := canonical.Hash(image.Canvas)
			expectedCanvas := node.CanvasHash
			geometry, found, geometryErr := s.geometry(ctx, who, tenantID, node)
			if geometryErr != nil {
				return geometryErr
			}
			if found {
				if sourceSHA == "" || image.Canvas.SourceSHA256 != sourceSHA {
					return v2input.ErrInvalid
				}
				chainHash, chainErr := canonical.Hash(image.TransformChain)
				if chainErr != nil || chainHash != geometry.TransformHash || expectedCanvas != "" && expectedCanvas != geometry.CanvasHash {
					return v2input.ErrInvalid
				}
				expectedCanvas = geometry.CanvasHash
			}
			if expectedCanvas == "" {
				expectedCanvas = binding.CanvasHash
			}
			if e != nil || canvasHash != expectedCanvas {
				return v2input.ErrInvalid
			}
			if node.Ref == binding.Source {
				sourceSHA = image.Canvas.SourceSHA256
				sourceTransform = image.TransformChain
			}
		case "CorrectionPlan":
			items, err := correctionBindings(body, binding)
			if err != nil {
				return err
			}
			for i, item := range items {
				children = append(children, LayerRef{
					Path:     node.Path + ".items[" + strconv.Itoa(i) + "].binding.source",
					Contract: "WorkingImage", Ref: item.Source, CanvasHash: item.CanvasHash})
			}
		case "Override":
			if sourceSHA == "" || editor.ValidateOverride(body, sourceSHA) != nil {
				return v2input.ErrInvalid
			}
		case "LayerStack":
			children, err = ValidateLayerStack(body, binding)
			if err != nil {
				return err
			}
		case "MaskRef":
			mask, e := review.ParseMaskRef(body)
			if e != nil {
				return v2input.ErrInvalid
			}
			canvasHash, e := canonical.Hash(mask.Canvas)
			expectedCanvas := binding.CanvasHash
			geometry, found, geometryErr := s.geometry(ctx, who, tenantID, node)
			if geometryErr != nil {
				return geometryErr
			}
			if found {
				if sourceSHA == "" || mask.Canvas.SourceSHA256 != sourceSHA {
					return v2input.ErrInvalid
				}
				chainHash, chainErr := canonical.Hash(mask.TransformChain)
				if chainErr != nil || chainHash != geometry.TransformHash {
					return v2input.ErrInvalid
				}
				expectedCanvas = geometry.CanvasHash
			}
			if e != nil || canvasHash != expectedCanvas {
				return v2input.ErrInvalid
			}
			if node.Path == "EditingRecipeV3.protected_mask" &&
				(!mask.Protected || !bytes.Equal(mask.TransformChain, sourceTransform)) {
				return v2input.ErrInvalid
			}
		}
		// Repeated image/mask occurrences still validate their canvas/protection
		// requirements. A generic Artifact encounter never proves typed schema.
		if repeated && sameValidation && node.RenderFrame == nil {
			return nil
		}
		validatedContracts[node.Ref.ArtifactID] = node.Contract
		if !compositionParams && node.Contract != "FaceObservations" && node.Contract != "DescriptorShard" && node.Contract != "CandidateProfile" && node.Contract != "FrozenRenderRecipe" && node.Contract != "LayerStack" && node.Contract != "CorrectionPlan" && node.Contract != "ImageResult" && node.Contract != "ImageReport" && node.Contract != "IdentityReport" {
			if err = nestedRefs(body, node.Path, node.Contract, node.Ref, 0, &children); err != nil {
				return err
			}
		}
		for _, child := range children {
			// Descendants retain the exact frame source; the root recipe's
			// selected asset version cannot stand in for other shard frames.
			if child.ExpectedBinding == nil && node.ExpectedBinding != nil {
				frameBinding := *node.ExpectedBinding
				child.ExpectedBinding = &frameBinding
			}
			if child.RenderFrame == nil && node.RenderFrame != nil {
				frameAuthority := *node.RenderFrame
				child.RenderFrame = &frameAuthority
			}
			if err = walk(child, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	for _, root := range roots {
		if err := walk(root, 0); err != nil {
			return nil, err
		}
	}
	return refs, nil
}
