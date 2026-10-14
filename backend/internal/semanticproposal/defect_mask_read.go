package semanticproposal

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
)

// ReadDefectFaceMaskRoots verifies the complete declared mask inventory through
// canonical producer catalog, original root bytes and bounded float32 NPY reads.
// Original Params and inline/external vectors are source/root/face checked.
// Skin source/producer documents and exact label-to-mask derivation are checked;
// qualified facial protection and transactional admission remain separate gates.
// Model qualification and actual inference are not certified by this reader.
func (s SQLTransferSourceReader) ReadDefectFaceMaskRoots(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins) ([]review.ProposalFaceMaskRoot, error) {
	return s.readDefectFaceMaskRoots(ctx, who, tenantID, binding, pins, nil)
}

type defectSkinVerifier func(context.Context, defectSkinReadProof) error

func (s SQLTransferSourceReader) readDefectFaceMaskRoots(ctx context.Context, who auth.Identity, tenantID uuid.UUID, binding v2input.SessionVersionBinding, pins DefectOriginPins, verify defectSkinVerifier) ([]review.ProposalFaceMaskRoot, error) {
	s = s.withManualRasterScope()
	evidence, err := s.ReadDefectEvidence(ctx, who, tenantID, binding, pins)
	if err != nil {
		return nil, err
	}
	coverageObjects, err := s.defectEvidenceObjects(ctx, who, tenantID, binding, pins)
	if err != nil {
		return nil, err
	}
	imageBody, err := s.Read(ctx, who, tenantID, binding)
	if err != nil {
		return nil, err
	}
	image, err := review.ParseWorkingImage(imageBody)
	if err != nil {
		return nil, err
	}
	type maskKey struct {
		Pixel    v2input.Ref
		Selector string
	}
	type retainedMask struct {
		Key     maskKey
		Objects defectMaskObjects
	}
	retained := make([]retainedMask, 0, len(evidence.FaceMasks))
	known := map[maskKey]defectMaskObjects{}
	type vectorPin struct {
		Pixel  v2input.Ref
		Object defectEvidenceObject
	}
	vectors := make([]vectorPin, 0)
	type manualOutputPin struct {
		Pixel   v2input.Ref
		Objects defectManualOutputs
	}
	manualOutputs := make([]manualOutputPin, 0)
	skins := make([]defectSkinReadProof, 0)
	result, err := review.ResolveProposalFaceMaskRoots(ctx, evidence.Report, review.ArtifactRef{ID: pins.Proposal.ArtifactID, SHA256: pins.Proposal.SHA256}, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}), func(ctx context.Context, mask review.ProposalFaceMask) (review.ArtifactRef, []byte, error) {
		key := maskKey{Pixel: v2input.Ref{ArtifactID: mask.Mask.Artifact.ID, SHA256: mask.Mask.Artifact.SHA256}, Selector: mask.Mask.Selector}
		objects, err := s.defectFaceMaskObjects(ctx, who, tenantID, binding, pins, key.Pixel, key.Selector)
		if err != nil {
			return review.ArtifactRef{}, nil, err
		}
		if old, exists := known[key]; exists && old != objects {
			return review.ArtifactRef{}, nil, v2input.ErrConflict
		}
		rootBody, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects.Root.object, objects.Root.ref)
		if err != nil {
			return review.ArtifactRef{}, nil, err
		}
		paramsBody, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, objects.Params.object, objects.Params.ref)
		if err != nil {
			return review.ArtifactRef{}, nil, err
		}
		if key.Selector == "skin" {
			proof, err := s.verifyDefectSkinPixels(ctx, who, tenantID, binding, pins, objects, paramsBody, mask.Mask, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
			if err != nil {
				return review.ArtifactRef{}, nil, err
			}
			if verify != nil {
				if err := verify(ctx, proof); err != nil {
					return review.ArtifactRef{}, nil, err
				}
			}
			skins = append(skins, proof)
		} else {
			external, err := review.VerifyManualMaskParams(paramsBody, review.ArtifactRef{ID: objects.Params.ref.ArtifactID, SHA256: objects.Params.ref.SHA256}, rootBody, review.ArtifactRef{ID: objects.Root.ref.ArtifactID, SHA256: objects.Root.ref.SHA256}, mask.Mask, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
			if err != nil {
				return review.ArtifactRef{}, nil, err
			}
			var originalVectorBody []byte
			if external != nil {
				vectorRef := v2input.Ref{ArtifactID: external.ID, SHA256: external.SHA256}
				vectorObject, err := s.defectManualVectorObject(ctx, who, tenantID, binding, pins, key.Pixel, vectorRef)
				if err != nil {
					return review.ArtifactRef{}, nil, err
				}
				vectorBody, err := v2input.ReadPinnedSemanticDocument(ctx, s.Objects, vectorObject.object, vectorRef)
				if err != nil {
					return review.ArtifactRef{}, nil, err
				}
				if err := review.VerifyManualVectorsDocument(vectorBody, *external, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256})); err != nil {
					return review.ArtifactRef{}, nil, err
				}
				originalVectorBody = vectorBody
				vectors = append(vectors, vectorPin{Pixel: key.Pixel, Object: vectorObject})
			}
			raster, err := review.ResolveManualRasterPlan(ctx, paramsBody, review.ArtifactRef{ID: objects.Params.ref.ArtifactID, SHA256: objects.Params.ref.SHA256}, rootBody, review.ArtifactRef{ID: objects.Root.ref.ArtifactID, SHA256: objects.Root.ref.SHA256}, mask.Mask, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}), originalVectorBody)
			if err != nil {
				return review.ArtifactRef{}, nil, err
			}
			if err := s.reserveManualRasterBudget(raster); err != nil {
				return review.ArtifactRef{}, nil, err
			}
			inventory, err := review.ResolveManualFaceMaskOutputs(rootBody, review.ArtifactRef{ID: objects.Root.ref.ArtifactID, SHA256: objects.Root.ref.SHA256}, mask.Mask, image.Source(review.ArtifactRef{ID: binding.Source.ArtifactID, SHA256: binding.Source.SHA256}))
			if err != nil {
				return review.ArtifactRef{}, nil, err
			}
			ref := func(r review.ArtifactRef) v2input.Ref { return v2input.Ref{ArtifactID: r.ID, SHA256: r.SHA256} }
			children, err := s.defectManualOutputObjects(ctx, who, tenantID, binding, pins, key.Pixel, [3]v2input.Ref{ref(inventory.Protected.Artifact), ref(inventory.TargetPNG), ref(inventory.ProtectedPNG)})
			if err != nil {
				return review.ArtifactRef{}, nil, err
			}
			if err := s.verifyProbabilityPNG(ctx, objects.Pixel, children[1], image.Canvas.Width, image.Canvas.Height); err != nil {
				return review.ArtifactRef{}, nil, err
			}
			if err := s.verifyProbabilityPNG(ctx, children[0], children[2], image.Canvas.Width, image.Canvas.Height); err != nil {
				return review.ArtifactRef{}, nil, err
			}
			if err := s.verifyManualRaster(ctx, objects.Pixel, children[0], raster); err != nil {
				return review.ArtifactRef{}, nil, err
			}
			if err := s.verifyManualProtectionCoverage(ctx, children[0], coverageObjects[4], image.Canvas.Width, image.Canvas.Height); err != nil {
				return review.ArtifactRef{}, nil, err
			}
			manualOutputs = append(manualOutputs, manualOutputPin{Pixel: key.Pixel, Objects: children})

		}
		if _, exists := known[key]; !exists {
			retained = append(retained, retainedMask{Key: key, Objects: objects})
		}
		known[key] = objects
		return review.ArtifactRef{ID: objects.Root.ref.ArtifactID, SHA256: objects.Root.ref.SHA256}, rootBody, nil
	})
	if err != nil {
		return nil, err
	}
	for _, entry := range retained {
		current, err := s.defectFaceMaskObjects(ctx, who, tenantID, binding, pins, entry.Key.Pixel, entry.Key.Selector)
		if err != nil {
			return nil, err
		}
		if current != entry.Objects {
			return nil, v2input.ErrConflict
		}
	}
	for _, proof := range skins {
		if verify != nil {
			if err := verify(ctx, proof); err != nil {
				return nil, err
			}
		}
		if err := s.recheckDefectSkin(ctx, who, tenantID, binding, pins, proof); err != nil {
			return nil, err
		}
	}
	if len(manualOutputs) > 0 {
		current, err := s.defectEvidenceObjects(ctx, who, tenantID, binding, pins)
		if err != nil {
			return nil, err
		}
		for i := range coverageObjects {
			if current[i] != coverageObjects[i] {
				return nil, v2input.ErrConflict
			}
		}
	}
	for _, entry := range manualOutputs {
		current, err := s.defectManualOutputObjects(ctx, who, tenantID, binding, pins, entry.Pixel, [3]v2input.Ref{entry.Objects[0].ref, entry.Objects[1].ref, entry.Objects[2].ref})
		if err != nil {
			return nil, err
		}
		if current != entry.Objects {
			return nil, v2input.ErrConflict
		}
	}
	for _, entry := range vectors {
		current, err := s.defectManualVectorObject(ctx, who, tenantID, binding, pins, entry.Pixel, entry.Object.ref)
		if err != nil {
			return nil, err
		}
		if current != entry.Object {
			return nil, v2input.ErrConflict
		}
	}
	if err := s.VerifyDefectOrigin(ctx, who, tenantID, binding, pins); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}
