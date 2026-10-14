package v2draft

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// DonorImageObject is catalog provenance for one physical occurrence, not
// evidence of binary content verification or live donor consent.
type DonorImageObject struct {
	Occurrence v2portrait.DonorImageOccurrence
	Object     objectstore.Object
}

// ResolveDonorImageObjects keeps the two version catalogs separate. Production
// callers supply SQLArtifactCatalog instances pinned to the corresponding
// project/version. Shared arrays are looked up independently under both scopes.
// Supplied manifest bytes are verified against Params; this function does not
// read manifests or binary bytes and must not be used as a complete closure.
func ResolveDonorImageObjects(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	params v2portrait.OpenEyesParams, sourceRaw, donorRaw []byte,
	baseCatalog, donorCatalog ArtifactCatalog) ([]DonorImageObject, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if tenant == uuid.Nil || baseCatalog == nil || donorCatalog == nil {
		return nil, v2input.ErrInvalid
	}
	// Includes cross-contract alias and conflicting digest rejection before IO.
	if _, err := v2portrait.DonorImageInputs(params, sourceRaw, donorRaw); err != nil {
		return nil, err
	}
	occurrences, err := v2portrait.DonorImageOccurrences(params, sourceRaw, donorRaw)
	if err != nil {
		return nil, err
	}
	for i, binding := range []v2input.SessionVersionBinding{params.Base, params.Donor} {
		catalog := baseCatalog
		if i == 1 {
			catalog = donorCatalog
		}
		b := binding
		object, err := catalog.Lookup(ctx, who, tenant, LayerRef{Contract: "WorkingImage", Ref: binding.Source, CanvasHash: binding.CanvasHash, ExpectedBinding: &b})
		if err != nil {
			return nil, err
		}
		if object.Bytes < 1 || object.SHA256 != binding.Source.SHA256 {
			return nil, v2input.ErrInvalid
		}
	}
	result := make([]DonorImageObject, 0, len(occurrences))
	for _, occurrence := range occurrences {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		catalog := baseCatalog
		if occurrence.Binding == params.Donor {
			catalog = donorCatalog
		}
		binding := occurrence.Binding
		object, err := catalog.Lookup(ctx, who, tenant, LayerRef{Contract: occurrence.Role, Ref: occurrence.Entry.Ref,
			ParentRef: occurrence.Parent, CanvasHash: binding.CanvasHash, ExpectedBinding: &binding})
		if err != nil {
			return nil, err
		}
		if object.Bytes < 1 || object.SHA256 != occurrence.Entry.Ref.SHA256 {
			return nil, v2input.ErrInvalid
		}
		result = append(result, DonorImageObject{Occurrence: occurrence, Object: object})
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

// DonorImageEvidence owns the two canonical manifest reads and the independently
// authorized physical occurrences. Binary bytes still require worker verification.
type DonorImageEvidence struct {
	SourceRaw []byte
	DonorRaw  []byte
	Physical  []DonorImageObject
}

// LoadDonorImageObjects reads only version-pinned, catalog-authorized manifests.
// All evidence is discarded if either read, manifest validation or occurrence
// lookup fails. No caller-provided manifest bytes enter this path.
func LoadDonorImageObjects(ctx context.Context, who auth.Identity, tenant uuid.UUID,
	params v2portrait.OpenEyesParams, objects v2input.SessionAnalysisObjectReader,
	baseCatalog, donorCatalog ArtifactCatalog) (DonorImageEvidence, error) {
	var zero DonorImageEvidence
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tenant == uuid.Nil || objects == nil || baseCatalog == nil || donorCatalog == nil {
		return zero, v2input.ErrInvalid
	}
	if _, _, err := params.Compile(); err != nil {
		return zero, err
	}
	bodies := make([][]byte, 2)
	for i, binding := range []v2input.SessionVersionBinding{params.Base, params.Donor} {
		catalog := baseCatalog
		if i == 1 {
			catalog = donorCatalog
		}
		b := binding
		object, err := catalog.Lookup(ctx, who, tenant, LayerRef{Contract: "WorkingImage", Ref: binding.Source, CanvasHash: binding.CanvasHash, ExpectedBinding: &b})
		if err != nil {
			return zero, err
		}
		if object.Bytes > 1<<20 {
			return zero, v2input.ErrInvalid
		}
		body, err := readJSONArtifact(ctx, objects, object, binding.Source)
		if err != nil {
			return zero, err
		}
		if _, err := v2portrait.VerifyDonorWorkingImage(binding, body); err != nil {
			return zero, err
		}
		bodies[i] = body
	}
	physical, err := ResolveDonorImageObjects(ctx, who, tenant, params, bodies[0], bodies[1], baseCatalog, donorCatalog)
	if err != nil {
		return zero, err
	}
	return DonorImageEvidence{SourceRaw: bodies[0], DonorRaw: bodies[1], Physical: physical}, nil
}

// LoadDonorImageEvidence uses the same project and separately pinned accepted
// versions. It supplies live SQL authorization for every occurrence, but does
// not replace donor consent/identity gates or graph admission revalidation.
func (s SQLArtifactCatalog) LoadDonorImageEvidence(ctx context.Context, who auth.Identity,
	tenant uuid.UUID, params v2portrait.OpenEyesParams, objects v2input.SessionAnalysisObjectReader) (DonorImageEvidence, error) {
	if s.AssetVersionID != params.Base.AssetVersionID {
		return DonorImageEvidence{}, v2input.ErrInvalid
	}
	donor := s
	donor.AssetVersionID = params.Donor.AssetVersionID
	return LoadDonorImageObjects(ctx, who, tenant, params, objects, s, donor)
}
