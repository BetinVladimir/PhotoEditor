package v2render

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
)

type adoptedLensCatalogSource interface {
	AdoptedLensObjectSource
	LoadChildObject(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, v2input.Ref, v2input.Ref, string, string, string) (objectstore.Object, error)
}

type adoptedOccurrence struct {
	ref            v2input.Ref
	contract, path string
}

// AdoptedLensCatalog records verified first-visit paths, never cached authority.
// Every Lookup repeats current SQL authorization, including repeated children.
type AdoptedLensCatalog struct {
	source                             adoptedLensCatalogSource
	original                           v2draft.ArtifactCatalog
	who                                auth.Identity
	tenant, project, version, producer uuid.UUID
	root                               v2input.Ref
	bucket                             string
	documents map[uuid.UUID][]byte
	entries                            map[uuid.UUID]adoptedOccurrence
	geometries                         map[uuid.UUID]v2draft.DocumentGeometry
}

func BuildAdoptedLensCatalog(ctx context.Context, source SQLAdoptedLensSource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, version, producer uuid.UUID, root v2input.Ref, bucket string) (*AdoptedLensCatalog, AdoptedLensBinding, error) {
	original := v2draft.SQLArtifactCatalog{DB: source.DB, Bucket: bucket, ProjectID: project, AssetVersionID: version}
	return buildAdoptedLensCatalog(ctx, source, original, objects, who, tenant, project, version, producer, root, bucket)
}

func buildAdoptedLensCatalog(ctx context.Context, source adoptedLensCatalogSource, original v2draft.ArtifactCatalog, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, version, producer uuid.UUID, root v2input.Ref, bucket string) (*AdoptedLensCatalog, AdoptedLensBinding, error) {
	binding, document, err := LoadAdoptedLensResult(ctx, source, objects, who, tenant, project, version, producer, root, bucket)
	if err != nil {
		return nil, AdoptedLensBinding{}, err
	}
	catalog := &AdoptedLensCatalog{source: source, original: original, who: who, tenant: tenant, project: project, version: version, producer: producer, root: root, bucket: bucket, documents:map[uuid.UUID][]byte{}, entries: map[uuid.UUID]adoptedOccurrence{}, geometries: map[uuid.UUID]v2draft.DocumentGeometry{}}
	active := map[uuid.UUID]bool{}
	var visit func(v2input.Ref, string, string, []byte, int) error
	visit = func(ref v2input.Ref, contract, path string, body []byte, depth int) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if depth > 32 || !validLayerArtifactRef(ref) || active[ref.ArtifactID] {
			return v2input.ErrInvalid
		}
		// Adoption stops at exact declared inputs; original-source descendants
		// are resolved separately through original project/version scope.
		if ref == binding.Base.Source {
			if contract != "WorkingImage" {
				return v2input.ErrInvalid
			}
			return nil
		}
		if prior, exists := catalog.entries[ref.ArtifactID]; exists {
			if prior.ref != ref || prior.contract != contract {
				return v2input.ErrInvalid
			}
			return nil
		}
		if len(catalog.entries) >= 512 {
			return v2input.ErrInvalid
		}
		catalog.entries[ref.ArtifactID] = adoptedOccurrence{ref, contract, path}
		active[ref.ArtifactID] = true
		defer delete(active, ref.ArtifactID)
		if body == nil {
			obj, err := source.LoadChildObject(ctx, who, tenant, project, version, producer, root, ref, path, contract, bucket)
			if err != nil {
				return err
			}
			media, limit, allowed := childObjectLimit(contract)
			if !allowed || obj.ContentType != media || obj.Bytes < 1 || obj.Bytes > limit || obj.SHA256 != ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
				return v2input.ErrInvalid
			}
			if media != "application/json" {
				return nil
			}
			reader, err := objects.Open(ctx, obj)
			if err != nil {
				return err
			}
			body, err = readAdoptedRoot(ctx, reader, obj.Bytes)
			closeErr := reader.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			sum := sha256.Sum256(body)
			jcs, err := canonical.Bytes(body)
			if err != nil || !bytes.Equal(jcs, body) || hex.EncodeToString(sum[:]) != ref.SHA256 {
				return v2input.ErrInvalid
			}
		}
		catalog.documents[ref.ArtifactID]=append([]byte(nil),body...)
		children, err := v2draft.DocumentChildren(body, contract, ref)
		if err != nil {
			return err
		}
		if contract == "WorkingImage" {
			image, err := review.ParseWorkingImage(body)
			if err != nil {
				return err
			}
			canvas, err := canonical.Hash(image.Canvas)
			if err != nil {
				return err
			}
			chain, err := canonical.Hash(image.TransformChain)
			if err != nil {
				return err
			}
			catalog.geometries[ref.ArtifactID] = v2draft.DocumentGeometry{CanvasHash: canvas, TransformHash: chain}
		}
		if contract == "MaskRef" {
			mask, err := review.ParseMaskRef(body)
			if err != nil {
				return err
			}
			canvas, err := canonical.Hash(mask.Canvas)
			if err != nil {
				return err
			}
			chain, err := canonical.Hash(mask.TransformChain)
			if err != nil {
				return err
			}
			catalog.geometries[ref.ArtifactID] = v2draft.DocumentGeometry{CanvasHash: canvas, TransformHash: chain}
		}
		for _, child := range children {
			childPath, err := adoptionChildPath(path, "Document", child.Path)
			if err != nil {
				return err
			}
			if err := visit(child.Ref, child.Contract, childPath, nil, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	body, err := canonical.Marshal(document)
	if err == nil {
		err = visit(root, "ImageResult", "$", body, 0)
	}
	if err != nil {
		return nil, AdoptedLensBinding{}, err
	}
	expected, ok := catalog.geometries[document.WorkingImage.ArtifactID]
	if !ok {
		if document.WorkingImage != binding.Base.Source || original == nil {
			return nil, AdoptedLensBinding{}, v2input.ErrInvalid
		}
		obj, err := original.Lookup(ctx, who, tenant, v2draft.LayerRef{Ref: binding.Base.Source, Contract: "WorkingImage", Path: "Original"})
		if err != nil {
			return nil, AdoptedLensBinding{}, err
		}
		if obj.ContentType != "application/json" || obj.Bytes < 2 || obj.Bytes > 8<<20 || obj.SHA256 != binding.Base.Source.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
			return nil, AdoptedLensBinding{}, v2input.ErrInvalid
		}
		reader, err := objects.Open(ctx, obj)
		if err != nil {
			return nil, AdoptedLensBinding{}, err
		}
		raw, readErr := readAdoptedRoot(ctx, reader, obj.Bytes)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, AdoptedLensBinding{}, readErr
		}
		if closeErr != nil {
			return nil, AdoptedLensBinding{}, closeErr
		}
		sum := sha256.Sum256(raw)
		jcs, err := canonical.Bytes(raw)
		if err != nil || !bytes.Equal(jcs, raw) || hex.EncodeToString(sum[:]) != binding.Base.Source.SHA256 {
			return nil, AdoptedLensBinding{}, v2input.ErrInvalid
		}
		image, err := review.ParseWorkingImage(raw)
		if err != nil {
			return nil, AdoptedLensBinding{}, err
		}
		canvas, err := canonical.Hash(image.Canvas)
		if err != nil || canvas != binding.Base.CanvasHash {
			return nil, AdoptedLensBinding{}, v2input.ErrInvalid
		}
		chain, err := canonical.Hash(image.TransformChain)
		if err != nil {
			return nil, AdoptedLensBinding{}, err
		}
		expected = v2draft.DocumentGeometry{CanvasHash: canvas, TransformHash: chain}
	}
	for _, geometry := range catalog.geometries {
		if geometry != expected {
			return nil, AdoptedLensBinding{}, v2input.ErrInvalid
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, AdoptedLensBinding{}, err
	}
	return catalog, binding, nil
}

func (c *AdoptedLensCatalog) Geometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (v2draft.DocumentGeometry, bool, error) {
	if err := ctx.Err(); err != nil {
		return v2draft.DocumentGeometry{}, false, err
	}
	if c == nil || tenant != c.tenant || who.UserID != c.who.UserID || who.SessionID != c.who.SessionID {
		return v2draft.DocumentGeometry{}, false, v2input.ErrInvalid
	}
	entry, exists := c.entries[node.Ref.ArtifactID]
	if !exists {
		return v2draft.DocumentGeometry{}, false, nil
	}
	if entry.ref != node.Ref || entry.contract != node.Contract {
		return v2draft.DocumentGeometry{}, false, v2input.ErrInvalid
	}
	geometry, exists := c.geometries[node.Ref.ArtifactID]
	return geometry, exists, nil
}

func (c *AdoptedLensCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	if c == nil || c.source == nil || c.original == nil || tenant != c.tenant || who.UserID != c.who.UserID || who.SessionID != c.who.SessionID {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if entry, ok := c.entries[node.Ref.ArtifactID]; ok {
		if entry.ref != node.Ref || entry.contract != node.Contract {
			return objectstore.Object{}, v2input.ErrInvalid
		}
		if node.Ref == c.root {
			_, obj, err := c.source.LoadObject(ctx, who, tenant, c.project, c.version, c.producer, c.root, c.bucket)
			return obj, err
		}
		return c.source.LoadChildObject(ctx, who, tenant, c.project, c.version, c.producer, c.root, node.Ref, entry.path, entry.contract, c.bucket)
	}
	return c.original.Lookup(ctx, who, tenant, node)
}
