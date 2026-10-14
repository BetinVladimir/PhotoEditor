package semanticproposal

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/masknpy"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
)

// compileDefectExecutionProfile composes retained original qualification and
// exact declared-file accounting. Sizes still require authorized ready catalog
// discovery and final transaction locks before any task may be queued.
func (s SQLTransferSourceReader) compileDefectExecutionProfile(ctx context.Context, who auth.Identity, tenant uuid.UUID, proof defectQualificationPreparationProof, expected v2qualification.Expected, width, height int64, published PublishedDefectParams, sizes []v2input.SemanticInputSize, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (json.RawMessage, error) {
	if _, err := s.marshalDefectQualificationPreparation(ctx, who, tenant, proof, expected); err != nil {
		return nil, err
	}
	if expected.Backend != "cpu" {
		return nil, v2qualification.ErrUnqualified
	}
	if err := validateTransferMeasuredCosts(costs, proof.metrics); err != nil {
		return nil, err
	}
	if _, err := validateDefectPublishedRefs(published); err != nil {
		return nil, err
	}
	inputs := make([]v2input.Ref, len(published.Inputs))
	for i, ref := range published.Inputs {
		inputs[i] = v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}
	}
	profile, err := v2input.CompileSemanticDefectProfileFromInputs(width, height, published.Ref, inputs, sizes, costs, ram, vram, scratch, output)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return profile, nil
}

// prepareCatalogDefectExecutionProfile owns descriptors and Params dependencies
// before catalog callbacks, requiring the exact declared set. Catalog contract
// authorization does not replace byte verification or final admission locks.
func (s SQLTransferSourceReader) prepareCatalogDefectExecutionProfile(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenant uuid.UUID, proof defectQualificationPreparationProof, expected v2qualification.Expected, width, height int64, published PublishedDefectParams, nodes []v2draft.LayerRef, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if catalog == nil || len(published.Inputs) == 0 || len(published.Inputs) > 4096 || len(nodes) != len(published.Inputs)+1 {
		return nil, v2input.ErrInvalid
	}
	expected = ownQualificationExpected(expected)
	body, err := canonical.Marshal(struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}{published, nodes})
	if err != nil || len(body) > 4<<20 {
		return nil, v2input.ErrInvalid
	}
	var owned struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}
	if json.Unmarshal(body, &owned) != nil || len(owned.Nodes) != len(owned.Published.Inputs)+1 {
		return nil, v2input.ErrInvalid
	}
	if _, err := validateDefectProfileDescriptors(owned.Published, owned.Nodes); err != nil {
		return nil, err
	}
	sizes := make([]v2input.SemanticInputSize, len(owned.Nodes))
	for i, node := range owned.Nodes {
		sizes[i] = v2input.SemanticInputSize{Ref: node.Ref, Bytes: 1}
	}
	// Preflight scope, qualification, cost lower bounds and dimensions before IO.
	if _, err := s.compileDefectExecutionProfile(ctx, who, tenant, proof, expected, width, height, owned.Published, sizes, costs, ram, vram, scratch, output); err != nil {
		return nil, err
	}
	actual, err := v2draft.ReadInputSizes(ctx, catalog, who, tenant, owned.Nodes)
	if err != nil {
		return nil, err
	}
	profile, err := s.compileDefectExecutionProfile(ctx, who, tenant, proof, expected, width, height, owned.Published, actual, costs, ram, vram, scratch, output)
	if err != nil {
		return nil, err
	}
	if err := s.recheckDefectQualificationPreparation(ctx, who, tenant, proof, expected); err != nil {
		return nil, err
	}
	return profile, nil
}

// prepareSourceDefectExecutionProfile derives dimensions from verified original
// ICC/RGB source bytes and requires its entire selected source closure among
// declared inputs. Face/mask completeness and final SQL locks remain separate.
func (s SQLTransferSourceReader) prepareSourceDefectExecutionProfile(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, report v2input.Ref, expected v2qualification.Expected, published PublishedDefectParams, nodes []v2draft.LayerRef, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (json.RawMessage, error) {
	prepared, err := s.prepareSourceDefectExecutionEvidence(ctx, catalog, who, tenant, binding, color, report, expected, published, nodes, costs, ram, vram, scratch, output)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(prepared.profile), nil
}

// Source-qualified evidence only: face/parser/manual producer and admission
// locking must accompany this value before any task may be queued.
type defectSourceExecutionPreparation struct {
	profile, descriptors, expected, files string
	source                                sourcePreparationProof
	qualification                         defectQualificationPreparationProof
}

func (s SQLTransferSourceReader) prepareSourceDefectExecutionEvidence(ctx context.Context, catalog v2draft.ArtifactCatalog, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, report v2input.Ref, expected v2qualification.Expected, published PublishedDefectParams, nodes []v2draft.LayerRef, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (defectSourceExecutionPreparation, error) {
	var zero defectSourceExecutionPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if catalog == nil || len(published.Inputs) == 0 || len(published.Inputs) > 4096 || len(nodes) != len(published.Inputs)+1 {
		return zero, v2input.ErrInvalid
	}
	expected = ownQualificationExpected(expected)
	body, err := canonical.Marshal(struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}{published, nodes})
	if err != nil || len(body) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	var owned struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}
	if json.Unmarshal(body, &owned) != nil {
		return zero, v2input.ErrInvalid
	}
	selected, err := validateDefectProfileDescriptors(owned.Published, owned.Nodes)
	if err != nil {
		return zero, err
	}
	qualification, err := s.readDefectExecutionQualification(ctx, who, tenant, report, expected, costs)
	if err != nil {
		return zero, err
	}
	source, proof, err := s.readQualifiedSourcePreparationProof(ctx, who, tenant, binding, color, true)
	if err != nil {
		return zero, err
	}
	pins, err := decodeSourcePreparationPins(proof)
	if err != nil {
		return zero, err
	}
	for _, pin := range pins {
		node, exists := selected[pin.Node.Ref.ArtifactID]
		if !exists {
			return zero, v2input.ErrInvalid
		}
		want, _ := canonical.Marshal(pin.Node)
		got, err := canonical.Marshal(node)
		if err != nil || string(want) != string(got) {
			return zero, v2input.ErrInvalid
		}
	}
	paramsNode, exists := selected[owned.Published.Ref.ArtifactID]
	if !exists || paramsNode.Contract != "Params" || catalog == nil {
		return zero, v2input.ErrInvalid
	}
	paramsObject, err := catalog.Lookup(ctx, who, tenant, paramsNode)
	if err != nil {
		return zero, err
	}
	paramsBody, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, paramsObject, owned.Published.Ref)
	if err != nil {
		return zero, err
	}
	sourceBody, err := s.Read(ctx, who, tenant, binding)
	if err != nil {
		return zero, err
	}
	image, err := review.ParseWorkingImage(sourceBody)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	imageRef := review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}
	paramsRef := review.ArtifactRef{ID: owned.Published.Ref.ArtifactID, SHA256: owned.Published.Ref.SHA256}
	if err := review.VerifyDefectProposalDeclaredInputs(paramsRef, paramsBody, image.Source(imageRef), owned.Published.Inputs); err != nil {
		return zero, v2input.ErrInvalid
	}
	// Check actual declared file costs before opening landmark/mask inputs.
	// The final catalog/profile check below still repeats after content IO.
	if _, err := s.prepareCatalogDefectExecutionProfile(ctx, catalog, who, tenant, qualification, expected, source.Width, source.Height, owned.Published, owned.Nodes, costs, ram, vram, scratch, output); err != nil {
		return zero, err
	}
	faces, err := review.DefectProposalFaceInputsFromParams(paramsRef, paramsBody, image.Source(imageRef))
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	landmarkDocuments := make(map[uuid.UUID]verifiedLandmarkDocument)
	for _, face := range faces {
		node, exists := selected[face.Landmarks.ID]
		if !exists || node.Contract != "FaceLandmarks" || node.Ref.SHA256 != face.Landmarks.SHA256 {
			return zero, v2input.ErrInvalid
		}
		object, err := catalog.Lookup(ctx, who, tenant, node)
		if err != nil {
			return zero, err
		}
		documentBody, err := s.readDefectLandmarkDocument(ctx, landmarkDocuments, node.Ref, object)
		if err != nil {
			return zero, err
		}
		if err := review.VerifyDefectProposalFaceDocument(face, documentBody, image.Source(imageRef)); err != nil {
			return zero, v2input.ErrInvalid
		}
		current, err := catalog.Lookup(ctx, who, tenant, node)
		if err != nil {
			return zero, err
		}
		if current != object {
			return zero, v2input.ErrConflict
		}
	}
	masks, err := review.DefectProposalMaskInputs(paramsRef, paramsBody, image.Source(imageRef))
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	checkedMasks := map[uuid.UUID]bool{}
	for _, mask := range masks {
		if checkedMasks[mask.Artifact.ID] {
			continue
		}
		node, exists := selected[mask.Artifact.ID]
		if !exists || node.Contract != "MaskPixels" || node.Ref.SHA256 != mask.Artifact.SHA256 {
			return zero, v2input.ErrInvalid
		}
		object, err := catalog.Lookup(ctx, who, tenant, node)
		if err != nil {
			return zero, err
		}
		if object.ContentType != "application/x-npy" && object.ContentType != "application/octet-stream" {
			return zero, v2input.ErrInvalid
		}
		stream, err := s.Objects.Open(ctx, object)
		if err != nil {
			return zero, err
		}
		verifyErr := masknpy.VerifyFloat32MaskNPY(ctx, stream, object.Bytes, node.Ref.SHA256, source.Width, source.Height)
		closeErr := stream.Close()
		if verifyErr != nil {
			return zero, verifyErr
		}
		if closeErr != nil {
			return zero, closeErr
		}
		current, err := catalog.Lookup(ctx, who, tenant, node)
		if err != nil {
			return zero, err
		}
		if current != object {
			return zero, v2input.ErrConflict
		}
		checkedMasks[mask.Artifact.ID] = true
	}
	currentParams, err := catalog.Lookup(ctx, who, tenant, paramsNode)
	if err != nil {
		return zero, err
	}
	if currentParams != paramsObject {
		return zero, v2input.ErrConflict
	}
	profile, err := s.prepareCatalogDefectExecutionProfile(ctx, catalog, who, tenant, qualification, expected, source.Width, source.Height, owned.Published, owned.Nodes, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	fileEvidence, err := v2draft.ReadInputEvidence(ctx, catalog, who, tenant, owned.Nodes)
	if err != nil {
		return zero, err
	}
	fileSizes := make([]v2input.SemanticInputSize, len(fileEvidence))
	for i, entry := range fileEvidence {
		fileSizes[i] = v2input.SemanticInputSize{Ref: entry.Ref, Bytes: entry.Object.Bytes}
	}
	accountedProfile, err := s.compileDefectExecutionProfile(ctx, who, tenant, qualification, expected, source.Width, source.Height, owned.Published, fileSizes, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	if string(accountedProfile) != string(profile) {
		return zero, v2input.ErrConflict
	}
	fileBody, err := canonical.Marshal(fileEvidence)
	if err != nil || len(fileBody) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	if err := s.recheckSourcePreparationProof(ctx, who, tenant, binding, color, true, proof); err != nil {
		return zero, err
	}
	if err := s.recheckDefectQualificationPreparation(ctx, who, tenant, qualification, expected); err != nil {
		return zero, err
	}
	expectedBody, err := canonical.Marshal(expected)
	if err != nil {
		return zero, err
	}
	prepared := defectSourceExecutionPreparation{profile: string(profile), files: string(fileBody), descriptors: string(body), expected: string(expectedBody), source: proof, qualification: qualification}
	if _, err := s.decodeDefectExecutionFilePins(ctx, who, tenant, binding, prepared); err != nil {
		return zero, err
	}
	return prepared, nil
}

// validateDefectProfileDescriptors checks exact identities before any IO.
func validateDefectProfileDescriptors(published PublishedDefectParams, nodes []v2draft.LayerRef) (map[uuid.UUID]v2draft.LayerRef, error) {
	if len(nodes) != len(published.Inputs)+1 {
		return nil, v2input.ErrInvalid
	}
	refs, err := validateDefectPublishedRefs(published)
	if err != nil {
		return nil, err
	}
	selected := make(map[uuid.UUID]v2draft.LayerRef, len(nodes))
	for _, node := range nodes {
		ref, exists := refs[node.Ref.ArtifactID]
		if !exists || ref != node.Ref || node.Contract == "" {
			return nil, v2input.ErrInvalid
		}
		if _, exists := selected[node.Ref.ArtifactID]; exists {
			return nil, v2input.ErrInvalid
		}
		selected[node.Ref.ArtifactID] = node
	}
	if selected[published.Ref.ArtifactID].Contract != "Params" {
		return nil, v2input.ErrInvalid
	}
	return selected, nil
}

// validateDefectPublishedRefs keeps provenance roots in the declared file set
// in both pure compilation and catalog-backed preparation.
func validateDefectPublishedRefs(published PublishedDefectParams) (map[uuid.UUID]v2input.Ref, error) {
	if len(published.Inputs) == 0 || len(published.Inputs) > 4096 || published.Ref.ArtifactID == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	if _, err := domain.ParseDigest(published.Ref.SHA256); err != nil {
		return nil, v2input.ErrInvalid
	}
	refs := map[uuid.UUID]v2input.Ref{published.Ref.ArtifactID: published.Ref}
	for _, ref := range published.Inputs {
		if ref.ID == uuid.Nil {
			return nil, v2input.ErrInvalid
		}
		if _, err := domain.ParseDigest(ref.SHA256); err != nil {
			return nil, v2input.ErrInvalid
		}
		if _, exists := refs[ref.ID]; exists {
			return nil, v2input.ErrInvalid
		}
		refs[ref.ID] = v2input.Ref{ArtifactID: ref.ID, SHA256: ref.SHA256}
	}
	// Manual roots carry provenance outside worker Params. They must remain
	// part of the accounted declared closure, never an untracked side channel.
	if len(published.ManualRoots) > len(published.Inputs) {
		return nil, v2input.ErrInvalid
	}
	manualRoots := make(map[uuid.UUID]bool, len(published.ManualRoots))
	for _, root := range published.ManualRoots {
		ref, exists := refs[root.ID]
		if !exists || root.ID == published.Ref.ArtifactID || ref.SHA256 != root.SHA256 || manualRoots[root.ID] {
			return nil, v2input.ErrInvalid
		}
		manualRoots[root.ID] = true
	}
	return refs, nil
}

type verifiedLandmarkDocument struct {
	object objectstore.Object
	body   string
}

// Cache is local to one preparation; every use follows a fresh catalog lookup.
func (s SQLTransferSourceReader) readDefectLandmarkDocument(ctx context.Context, documents map[uuid.UUID]verifiedLandmarkDocument, ref v2input.Ref, object objectstore.Object) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if documents == nil {
		return nil, v2input.ErrInvalid
	}
	if document, cached := documents[ref.ArtifactID]; cached {
		if document.object != object || object.SHA256 != ref.SHA256 {
			return nil, v2input.ErrConflict
		}
		return []byte(document.body), nil
	}
	body, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, object, ref)
	if err != nil {
		return nil, err
	}
	documents[ref.ArtifactID] = verifiedLandmarkDocument{object: object, body: string(body)}
	return body, nil
}

// prepareFaceQualifiedDefectExecutionProfile composes original face producer
// qualification with byte/resource preparation for an existing canonical origin.
// Both qualification passes are mandatory; final admission locks remain separate.
func (s SQLTransferSourceReader) prepareFaceQualifiedDefectExecutionProfile(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, pins DefectOriginPins, faceReports [2]v2input.Ref, faceExpected [2]v2qualification.Expected, report v2input.Ref, expected v2qualification.Expected, published PublishedDefectParams, nodes []v2draft.LayerRef, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (json.RawMessage, error) {
	prepared, err := s.prepareFaceQualifiedDefectExecutionEvidence(ctx, who, tenant, binding, color, pins, faceReports, faceExpected, report, expected, published, nodes, costs, ram, vram, scratch, output)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(prepared.source.profile), nil
}

// Private original source/face preparation, not final queue authority.
type defectFaceExecutionPreparation struct {
	source          defectSourceExecutionPreparation
	origin          DefectOriginPins
	originProof     defectOriginPreparationProof
	faces, expected string
	qualification   [2]personEvidenceObject
	producers       [64]facePreparationProof
	producerCount   int
}

func (s SQLTransferSourceReader) prepareFaceQualifiedDefectExecutionEvidence(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, pins DefectOriginPins, faceReports [2]v2input.Ref, faceExpected [2]v2qualification.Expected, report v2input.Ref, expected v2qualification.Expected, published PublishedDefectParams, nodes []v2draft.LayerRef, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (defectFaceExecutionPreparation, error) {
	var zero defectFaceExecutionPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if published.Ref != pins.Params {
		return zero, v2input.ErrInvalid
	}
	if _, err := validateDefectProfileDescriptors(published, nodes); err != nil {
		return zero, err
	}
	if err := validateDefectProfileQualificationFamilies(expected, faceExpected, nil); err != nil {
		return zero, err
	}
	ownedBody, err := canonical.Marshal(struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}{published, nodes})
	if err != nil || len(ownedBody) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	var owned struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}
	if json.Unmarshal(ownedBody, &owned) != nil {
		return zero, v2input.ErrInvalid
	}
	published, nodes = owned.Published, owned.Nodes
	expected = ownQualificationExpected(expected)
	for i := range faceExpected {
		faceExpected[i] = ownQualificationExpected(faceExpected[i])
	}
	qualification, err := s.readDefectExecutionQualification(ctx, who, tenant, report, expected, costs)
	if err != nil {
		return zero, err
	}
	_, sourceProof, err := s.readQualifiedSourcePreparationProof(ctx, who, tenant, binding, color, true)
	if err != nil {
		return zero, err
	}
	originProof, err := s.readDefectOriginPreparationProof(ctx, who, tenant, binding, pins)
	if err != nil {
		return zero, err
	}
	var faceQualification [2]personEvidenceObject
	for i := range faceReports {
		pin, err := s.faceQualificationObject(ctx, who, tenant, faceExpected[i], faceReports[i])
		if err != nil {
			return zero, err
		}
		faceQualification[i] = pin
	}
	faces, producers, err := s.readQualifiedDefectFaceEvidence(ctx, who, tenant, binding, pins, faceReports, faceExpected)
	if err != nil {
		return zero, err
	}
	if len(producers) > 64 {
		return zero, v2input.ErrInvalid
	}
	before, err := canonical.Marshal(faces)
	if err != nil {
		return zero, err
	}
	catalog := defectProfileCatalog{reader: s, binding: binding, pins: pins}
	preparedSource, err := s.prepareSourceDefectExecutionEvidence(ctx, catalog, who, tenant, binding, color, report, expected, published, nodes, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	current, err := s.ReadQualifiedDefectFaceSet(ctx, who, tenant, binding, pins, faceReports, faceExpected)
	if err != nil {
		return zero, err
	}
	after, err := canonical.Marshal(current)
	if err != nil || string(before) != string(after) {
		return zero, v2input.ErrConflict
	}
	for i := range faceReports {
		pin, err := s.faceQualificationObject(ctx, who, tenant, faceExpected[i], faceReports[i])
		if err != nil {
			return zero, err
		}
		if pin != faceQualification[i] {
			return zero, v2qualification.ErrUnqualified
		}
	}
	if err := s.recheckSourcePreparationProof(ctx, who, tenant, binding, color, true, sourceProof); err != nil {
		return zero, err
	}
	if err := s.recheckDefectQualificationPreparation(ctx, who, tenant, qualification, expected); err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	faceExpectedBody, err := canonical.Marshal(faceExpected)
	if err != nil {
		return zero, err
	}

	for _, proof := range producers {
		if err := s.recheckFacePreparationProof(ctx, who, tenant, binding, proof); err != nil {
			return zero, err
		}
	}
	if err := s.recheckDefectOriginPreparationProof(ctx, who, tenant, binding, originProof); err != nil {
		return zero, err
	}
	result := defectFaceExecutionPreparation{source: preparedSource, origin: pins, originProof: originProof, faces: string(before), expected: string(faceExpectedBody), qualification: faceQualification, producerCount: len(producers)}
	copy(result.producers[:], producers)
	return result, nil
}

// prepareMaskQualifiedDefectExecutionProfile adds complete original mask-root
// and parser qualification checks around the face/source profile composition.
// It remains preparation of an existing canonical origin, not queue authority.
func (s SQLTransferSourceReader) prepareMaskQualifiedDefectExecutionProfile(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, pins DefectOriginPins, faceReports, maskReports [2]v2input.Ref, faceExpected, maskExpected [2]v2qualification.Expected, report v2input.Ref, expected v2qualification.Expected, published PublishedDefectParams, nodes []v2draft.LayerRef, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (json.RawMessage, error) {
	prepared, err := s.prepareMaskQualifiedDefectExecutionEvidence(ctx, who, tenant, binding, color, pins, faceReports, maskReports, faceExpected, maskExpected, report, expected, published, nodes, costs, ram, vram, scratch, output)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(prepared.face.source.profile), nil
}

// Private composed preparation; original producer location proofs and final
// transaction locking are still required before queue admission.
type defectMaskExecutionPreparation struct {
	face            defectFaceExecutionPreparation
	roots, expected string
	qualification   [4]personEvidenceObject
	parsers         [64]faceParsePreparationProof
	parserCount     int
	manual          [64]defectManualExecutionProof
	manualCount     int
}

func (s SQLTransferSourceReader) prepareMaskQualifiedDefectExecutionEvidence(ctx context.Context, who auth.Identity, tenant uuid.UUID, binding v2input.SessionVersionBinding, color TransferColorProfile, pins DefectOriginPins, faceReports, maskReports [2]v2input.Ref, faceExpected, maskExpected [2]v2qualification.Expected, report v2input.Ref, expected v2qualification.Expected, published PublishedDefectParams, nodes []v2draft.LayerRef, costs v2input.SemanticQualifiedCosts, ram, vram, scratch, output int64) (defectMaskExecutionPreparation, error) {
	var zero defectMaskExecutionPreparation
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if published.Ref != pins.Params {
		return zero, v2input.ErrInvalid
	}
	if _, err := validateDefectProfileDescriptors(published, nodes); err != nil {
		return zero, err
	}
	if err := validateDefectProfileQualificationFamilies(expected, faceExpected, &maskExpected); err != nil {
		return zero, err
	}
	for i := range maskExpected {
		maskExpected[i] = ownQualificationExpected(maskExpected[i])
		faceExpected[i] = ownQualificationExpected(faceExpected[i])
	}
	if maskExpected[0].Operation != "face.parse" || maskExpected[1].Operation != "face.detect" {
		return zero, v2qualification.ErrUnqualified
	}
	body, err := canonical.Marshal(struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}{published, nodes})
	if err != nil || len(body) > 4<<20 {
		return zero, v2input.ErrInvalid
	}
	var owned struct {
		Published PublishedDefectParams
		Nodes     []v2draft.LayerRef
	}
	if json.Unmarshal(body, &owned) != nil {
		return zero, v2input.ErrInvalid
	}
	expected = ownQualificationExpected(expected)
	qualification, err := s.readDefectExecutionQualification(ctx, who, tenant, report, expected, costs)
	if err != nil {
		return zero, err
	}
	_, source, err := s.readQualifiedSourcePreparationProof(ctx, who, tenant, binding, color, true)
	if err != nil {
		return zero, err
	}
	qualificationReports := [4]v2input.Ref{faceReports[0], faceReports[1], maskReports[0], maskReports[1]}
	qualificationExpected := [4]v2qualification.Expected{faceExpected[0], faceExpected[1], maskExpected[0], maskExpected[1]}
	var qualificationObjects [4]personEvidenceObject
	for i := range qualificationReports {
		pin, err := s.faceQualificationObject(ctx, who, tenant, qualificationExpected[i], qualificationReports[i])
		if err != nil {
			return zero, err
		}
		qualificationObjects[i] = pin
	}
	roots, parsers, err := s.readQualifiedDefectFaceMaskEvidence(ctx, who, tenant, binding, pins, maskReports, maskExpected, color)
	if err != nil {
		return zero, err
	}
	selected, err := validateDefectProfileDescriptors(owned.Published, owned.Nodes)
	if err != nil {
		return zero, err
	}
	for _, root := range roots {
		for _, ref := range []review.ArtifactRef{root.Root, root.Binding.Mask.Artifact} {
			node, exists := selected[ref.ID]
			if !exists || node.Ref.SHA256 != ref.SHA256 {
				return zero, v2input.ErrInvalid
			}
		}
	}
	before, err := canonical.Marshal(roots)
	if err != nil {
		return zero, err
	}
	manual, err := s.readDefectManualExecutionProofs(ctx, who, tenant, binding, roots)
	if err != nil {
		return zero, err
	}
	preparedFace, err := s.prepareFaceQualifiedDefectExecutionEvidence(ctx, who, tenant, binding, color, pins, faceReports, faceExpected, report, expected, owned.Published, owned.Nodes, costs, ram, vram, scratch, output)
	if err != nil {
		return zero, err
	}
	current, err := s.ReadQualifiedDefectFaceMaskRoots(ctx, who, tenant, binding, pins, maskReports, maskExpected, color)
	if err != nil {
		return zero, err
	}
	after, err := canonical.Marshal(current)
	if err != nil || string(before) != string(after) {
		return zero, v2input.ErrConflict
	}
	// Mask IO follows inner face validation: repeat producer/profile authority
	// before final retained qualification/source checks.
	if _, err := s.ReadQualifiedDefectFaceSet(ctx, who, tenant, binding, pins, faceReports, faceExpected); err != nil {
		return zero, err
	}
	for i := range qualificationReports {
		pin, err := s.faceQualificationObject(ctx, who, tenant, qualificationExpected[i], qualificationReports[i])
		if err != nil {
			return zero, err
		}
		if pin != qualificationObjects[i] {
			return zero, v2qualification.ErrUnqualified
		}
	}
	if err := s.recheckSourcePreparationProof(ctx, who, tenant, binding, color, true, source); err != nil {
		return zero, err
	}
	if err := s.recheckDefectQualificationPreparation(ctx, who, tenant, qualification, expected); err != nil {
		return zero, err
	}
	expectedBody, err := canonical.Marshal(qualificationExpected)
	if err != nil {
		return zero, err
	}
	if err := ctx.Err(); err != nil {
		return zero, err
	}

	if len(parsers) > 64 {
		return zero, v2input.ErrInvalid
	}
	for _, proof := range parsers {
		if err := s.recheckFaceParsePreparationProof(ctx, who, tenant, binding, proof); err != nil {
			return zero, err
		}
	}
	for _, proof := range manual {
		if err := s.recheckManualPreparationProof(ctx, who, tenant, binding, proof.original()); err != nil {
			return zero, err
		}
	}
	if err := s.recheckDefectOriginPreparationProof(ctx, who, tenant, binding, preparedFace.originProof); err != nil {
		return zero, err
	}
	result := defectMaskExecutionPreparation{face: preparedFace, roots: string(before), expected: string(expectedBody), qualification: qualificationObjects, parserCount: len(parsers), manualCount: len(manual)}
	copy(result.parsers[:], parsers)
	copy(result.manual[:], manual)
	return result, nil
}

// Family contracts are checked before any qualification/source IO.
func validateDefectProfileQualificationFamilies(defect v2qualification.Expected, faces [2]v2qualification.Expected, masks *[2]v2qualification.Expected) error {
	if defect.Operation != "defects.propose" || defect.Version != "1" || defect.Backend != "cpu" {
		return v2qualification.ErrUnqualified
	}
	for i, operation := range [2]string{"face.landmarks", "face.detect"} {
		if faces[i].Operation != operation || faces[i].Version != "1" {
			return v2qualification.ErrUnqualified
		}
	}
	if masks != nil {
		for i, operation := range [2]string{"face.parse", "face.detect"} {
			if masks[i].Operation != operation || masks[i].Version != "1" {
				return v2qualification.ErrUnqualified
			}
		}
	}
	return nil
}
