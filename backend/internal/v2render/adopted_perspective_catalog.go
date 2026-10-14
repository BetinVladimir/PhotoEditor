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

type adoptedPerspectiveCatalogSource interface {
	AdoptedPerspectiveObjectSource
	LoadChildObject(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, v2input.Ref, v2input.Ref, string, string, string) (objectstore.Object, error)
}

// AdoptedPerspectiveCatalog records verified first-visit paths, never cached authority.
// Every Lookup repeats current SQL authorization, including repeated children.
type AdoptedPerspectiveCatalog struct {
	documents                          map[string][]byte
	source                             adoptedPerspectiveCatalogSource
	original                           v2draft.ArtifactCatalog
	who                                auth.Identity
	tenant, project, version, producer uuid.UUID
	root                               v2input.Ref
	bucket                             string
	entries                            map[uuid.UUID]adoptedOccurrence
	geometries                         map[uuid.UUID]v2draft.DocumentGeometry
	originalFields                     map[uuid.UUID]v2draft.LayerRef
}

func BuildAdoptedPerspectiveCatalog(ctx context.Context, source SQLAdoptedPerspectiveSource, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, version, producer uuid.UUID, root v2input.Ref, bucket string) (*AdoptedPerspectiveCatalog, AdoptedPerspectiveBinding, error) {
	original := &perspectiveSourceCatalog{source: source, fallback: v2draft.SQLArtifactCatalog{DB: source.DB, Bucket: bucket, ProjectID: project, AssetVersionID: version}, project: project, version: version, producer: producer, root: root, bucket: bucket}
	return buildAdoptedPerspectiveCatalog(ctx, source, original, objects, who, tenant, project, version, producer, root, bucket)
}

func buildAdoptedPerspectiveCatalog(ctx context.Context, source adoptedPerspectiveCatalogSource, original v2draft.ArtifactCatalog, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, version, producer uuid.UUID, root v2input.Ref, bucket string) (*AdoptedPerspectiveCatalog, AdoptedPerspectiveBinding, error) {
	binding, document, err := LoadAdoptedPerspectiveResult(ctx, source, objects, who, tenant, project, version, producer, root, bucket)
	if err != nil {
		return nil, AdoptedPerspectiveBinding{}, err
	}
	catalog := &AdoptedPerspectiveCatalog{source: source, original: original, who: who, tenant: tenant, project: project, version: version, producer: producer, root: root, bucket: bucket, entries: map[uuid.UUID]adoptedOccurrence{}, geometries: map[uuid.UUID]v2draft.DocumentGeometry{}}
	sourceRaw, err := readPerspectiveGeometrySource(ctx, original, objects, who, tenant, binding.Base.Source)
	if err != nil {
		return nil, AdoptedPerspectiveBinding{}, err
	}
	if scoped,ok:=original.(*perspectiveSourceCatalog);ok {
 scoped.sourceRef=binding.Base.Source
 scoped.sourceRaw=bytes.Clone(sourceRaw)
 }
 catalog.originalFields, err = perspectiveOriginalDenseFields(sourceRaw, binding.Base.Source)
	if err != nil {
		return nil, AdoptedPerspectiveBinding{}, err
	}
	documents := map[string][]byte{}
	catalog.documents = documents
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
		if field, exists := catalog.originalFields[ref.ArtifactID]; exists {
			if ref != field.Ref || contract != field.Contract {
				return v2input.ErrInvalid
			}
			_, err := resolvePerspectiveOriginalField(ctx, original, who, tenant, field)
			return err
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
			media, limit, allowed := perspectiveChildObjectLimit(contract)
			if !allowed || obj.ContentType != media || obj.Bytes < 1 || obj.Bytes > limit || obj.SHA256 != ref.SHA256 || obj.Key == "" || obj.VersionID == "" || obj.VersionID == "null" {
				return v2input.ErrInvalid
			}
			if media != "application/json" {
				return nil
			}
			reader, err := objects.Open(ctx, obj)
			if err != nil {
				if reader != nil {
					_ = reader.Close()
				}
				return err
			}
			if reader == nil {
				return v2input.ErrInvalid
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
			current, err := source.LoadChildObject(ctx, who, tenant, project, version, producer, root, ref, path, contract, bucket)
			if err != nil {
				return err
			}
			if current.Key != obj.Key || current.VersionID != obj.VersionID || current.Bytes != obj.Bytes || current.SHA256 != obj.SHA256 || current.ContentType != obj.ContentType {
				return v2input.ErrExpired
			}
		}
		documents[ref.ArtifactID.String()] = bytes.Clone(body)
		var children []v2draft.LayerRef
		var err error
		if contract == "Artifact" {
			if path != "$/report/transform" {
				return v2input.ErrInvalid
			}
			if err := validatePerspectiveResolvedTransform(body); err != nil {
				return err
			}
		} else {
			children, err = v2draft.DocumentChildren(body, contract, ref)
		}
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
		return nil, AdoptedPerspectiveBinding{}, err
	}
	documents[binding.Base.Source.ArtifactID.String()] = bytes.Clone(sourceRaw)
	if err := verifyPerspectiveResultGeometry(binding, document, sourceRaw, documents); err != nil {
		return nil, AdoptedPerspectiveBinding{}, err
	}
	expected, ok := catalog.geometries[document.WorkingImage.ArtifactID]
	if !ok {
		if document.WorkingImage != binding.Base.Source || original == nil {
			return nil, AdoptedPerspectiveBinding{}, v2input.ErrInvalid
		}
		image, err := review.ParseWorkingImage(sourceRaw)
		if err != nil {
			return nil, AdoptedPerspectiveBinding{}, err
		}
		canvas, err := canonical.Hash(image.Canvas)
		if err != nil || canvas != binding.Base.CanvasHash {
			return nil, AdoptedPerspectiveBinding{}, v2input.ErrInvalid
		}
		chain, err := canonical.Hash(image.TransformChain)
		if err != nil {
			return nil, AdoptedPerspectiveBinding{}, err
		}
		expected = v2draft.DocumentGeometry{CanvasHash: canvas, TransformHash: chain}
	}
	for _, geometry := range catalog.geometries {
		if geometry != expected {
			return nil, AdoptedPerspectiveBinding{}, v2input.ErrInvalid
		}
	}
	// Reused fields retain original authority even after all output document IO.
	for _, field := range catalog.originalFields {
		if _, err := resolvePerspectiveOriginalField(ctx, original, who, tenant, field); err != nil {
			return nil, AdoptedPerspectiveBinding{}, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, AdoptedPerspectiveBinding{}, err
	}
	// Child and source IO may outlive the initial root observation. Re-read the
	// complete root and binding after all descendants, before exposing a catalog.
	currentBinding,currentDocument,err:=LoadAdoptedPerspectiveResult(ctx,source,objects,who,tenant,project,version,producer,root,bucket)
	if err!=nil{return nil,AdoptedPerspectiveBinding{},err}
	currentBody,err:=canonical.Marshal(currentDocument)
	if err!=nil{return nil,AdoptedPerspectiveBinding{},err}
	if currentBinding!=binding||!bytes.Equal(currentBody,body){return nil,AdoptedPerspectiveBinding{},v2input.ErrExpired}
	return catalog, binding, nil
}

func (c *AdoptedPerspectiveCatalog) Geometry(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (v2draft.DocumentGeometry, bool, error) {
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

func (c *AdoptedPerspectiveCatalog) Lookup(ctx context.Context, who auth.Identity, tenant uuid.UUID, node v2draft.LayerRef) (objectstore.Object, error) {
	if c == nil || c.source == nil || c.original == nil || tenant != c.tenant || who.UserID != c.who.UserID || who.SessionID != c.who.SessionID {
		return objectstore.Object{}, v2input.ErrInvalid
	}
	if field, ok := c.originalFields[node.Ref.ArtifactID]; ok {
		if field.Ref != node.Ref || field.Contract != node.Contract {
			return objectstore.Object{}, v2input.ErrInvalid
		}
		return resolvePerspectiveOriginalField(ctx, c.original, who, tenant, field)
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

// Source documents have adopted-producer authority. Other original descendants
// retain their existing version scope until separately authorized.
type perspectiveInputObjectSource interface {
 LoadSourceObject(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID,v2input.Ref,v2input.Ref,string)(objectstore.Object,error)
 LoadSourceField(context.Context,auth.Identity,uuid.UUID,uuid.UUID,uuid.UUID,uuid.UUID,v2input.Ref,v2input.Ref,v2input.Ref,[]byte,string,string)(objectstore.Object,error)
}
type perspectiveSourceCatalog struct {
 source perspectiveInputObjectSource
 fallback v2draft.ArtifactCatalog
 project,version,producer uuid.UUID
 root v2input.Ref
 bucket string
 sourceRef v2input.Ref
 sourceRaw []byte
}
func(c *perspectiveSourceCatalog) Lookup(ctx context.Context,who auth.Identity,tenant uuid.UUID,node v2draft.LayerRef)(objectstore.Object,error){
 if err:=ctx.Err();err!=nil{return objectstore.Object{},err}
 if c==nil||c.source==nil{return objectstore.Object{},v2input.ErrInvalid}
 if node.Contract=="WorkingImage" && node.Path=="Original" {
  return c.source.LoadSourceObject(ctx,who,tenant,c.project,c.version,c.producer,c.root,node.Ref,c.bucket)
 }
 if node.Contract=="DenseWarpField" && len(c.sourceRaw)>0 {
  fields,err:=perspectiveOriginalDenseFields(c.sourceRaw,c.sourceRef)
  if err!=nil{return objectstore.Object{},err}
  expected,ok:=fields[node.Ref.ArtifactID]
  if !ok||expected!=node{return objectstore.Object{},v2input.ErrInvalid}
  return c.source.LoadSourceField(ctx,who,tenant,c.project,c.version,c.producer,c.root,c.sourceRef,node.Ref,c.sourceRaw,node.Path,c.bucket)
 }
 if c.fallback==nil{return objectstore.Object{},v2input.ErrInvalid}
 return c.fallback.Lookup(ctx,who,tenant,node)
}
