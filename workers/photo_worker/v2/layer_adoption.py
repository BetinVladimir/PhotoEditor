"""Evidence-scoped composition metadata proof before relational adoption."""
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.layers.contracts import LayerComposeParams
from photo_worker.operations.layers.output_validation import validate_layer_output_metadata
from photo_worker.operations.optical.contracts import ImageResult
from .adoption import AdoptionInvalid
from .lens_bypass import _document, _ref


def verify_layer_metadata(reader, params_ref, root_ref, *, check_active=lambda:None):
    """Params must come from task metadata. Binary and SQL proofs remain required."""
    check_active()
    params=_document(reader,params_ref,LayerComposeParams,'input')
    result=_document(reader,root_ref,ImageResult,'output')
    source=_document(reader,_ref(params.base.source),WorkingImage,'input')
    source_parent=(_document(reader,_ref(params.source_result),ImageResult,'input')
                   if params.source_result is not None else None)
    proposal_parent=_document(reader,_ref(params.proposal_result),ImageResult,'input')
    class Artifacts:
        def contract(self,reference,model):
            check_active()
            ref=_ref(reference)
            evidence=reader.evidence(ref)
            if evidence.source not in ('input','output'):
                raise AdoptionInvalid('LAYER_ADOPTION_EVIDENCE_INVALID')
            return _document(reader,ref,model,evidence.source)
    try:
        validate_layer_output_metadata(Artifacts(),params,result,source,source_parent,proposal_parent)
    except ValueError as error:
        raise AdoptionInvalid('LAYER_ADOPTION_METADATA_INVALID') from error
    check_active()
    return params,result


def verify_layer_output_arrays(reader, params_ref, root_ref, *, check_active=lambda:None):
    """Verify canonical output NPY bytes, hash, shape and binary mask values.

    Does not yet establish exact inherited coverage or RGB composition equality.
    """
    from photo_worker.contracts.images import MaskRef
    from .lens_adoption import verify_lens_array
    params,result=verify_layer_metadata(reader,params_ref,root_ref,check_active=check_active)
    output=_document(reader,_ref(result.working_image),WorkingImage,
                     reader.evidence(_ref(result.working_image)).source)
    shape=(output.canvas.height,output.canvas.width)
    if shape[0]*shape[1]>200000000:
        raise AdoptionInvalid('LAYER_ADOPTION_PIXEL_LIMIT')
    for reference in (result.changed_mask,result.generated_mask):
        check_active()
        if reference is None:continue
        mask=_document(reader,_ref(reference),MaskRef,'output')
        verify_lens_array(reader,_ref(mask.artifact),shape,'mask_pixels',unit=True,binary=True,check_active=check_active)
    for reference,dimensions,role,unit in ((output.pixels,(*shape,3),'pixels',False),
                                         (output.alpha,shape,'alpha',True)):
        check_active()
        if reference is None:continue
        evidence=reader.evidence(_ref(reference))
        if evidence.source=='output':
            verify_lens_array(reader,_ref(reference),dimensions,role,unit=unit,check_active=check_active)
        elif evidence.source!='input':
            raise AdoptionInvalid('LAYER_ADOPTION_EVIDENCE_INVALID')
    check_active()


def verify_layer_composition(reader,params_ref,root_ref,*,scratch_root,max_bytes,alpha_mode="require_float32",check_active=lambda:None):
    if alpha_mode not in ("require_float32","normalize_uint16"):
        raise AdoptionInvalid("LAYER_ADOPTION_ALPHA_POLICY_INVALID")
    from .array_evidence import ArrayEvidenceSession
    from photo_worker.contracts.images import MaskRef
    from photo_worker.algorithms.layer_masks import compile_mask,evaluate_mask_tile
    from photo_worker.operations.layers.output_validation import validate_composed_pixels,validate_generated_coverage
    params,result=verify_layer_metadata(reader,params_ref,root_ref,check_active=check_active)
    def document(ref,model,source='input'):
        return _document(reader,_ref(ref),model,source)
    source=document(params.base.source,WorkingImage)
    parent=document(params.source_result,ImageResult) if params.source_result else None
    proposal_parent=document(params.proposal_result,ImageResult)
    proposal=document(proposal_parent.working_image,WorkingImage)
    if (proposal.canvas!=source.canvas or proposal.color!=source.color or proposal.dtype!=source.dtype
            or proposal.transform_chain!=source.transform_chain):
        raise AdoptionInvalid('LAYER_ADOPTION_PARENT_GEOMETRY_INVALID')
    output=document(result.working_image,WorkingImage,reader.evidence(_ref(result.working_image)).source)
    shape=(source.canvas.height,source.canvas.width)
    plan=compile_mask(params.mask,params.base.canvas_hash)
    try:
        with ArrayEvidenceSession(reader,scratch_root,max_bytes,check_active) as arrays:
            def array(ref,dimensions=shape,unit=False):
                if ref is None:return None
                evidence=reader.evidence(_ref(ref))
                return arrays.array(_ref(ref),dimensions,source=evidence.source,unit=unit)
            def mask(ref,scope='input'):
                if ref is None:return None
                value=document(ref,MaskRef,scope)
                if value.canvas!=source.canvas or value.transform_chain!=source.transform_chain:
                    raise AdoptionInvalid('LAYER_ADOPTION_MASK_GEOMETRY_INVALID')
                return array(value.artifact,unit=True)
            masks={}
            for node in plan.nodes:
                if node.operation=='ref':masks[(node.ref.artifact_id,node.ref.sha256)]=mask(node.ref)
            effective=arrays.allocate_mask(shape)
            def read_tile(ref,x,y,width,height):
                return masks[(ref.artifact_id,ref.sha256)][y:y+height,x:x+width]
            for y in range(0,shape[0],512):
                for x in range(0,shape[1],512):
                    check_active();h=min(512,shape[0]-y);w=min(512,shape[1]-x)
                    effective[y:y+h,x:x+w]=evaluate_mask_tile(plan,read_tile,x=x,y=y,width=w,height=h,
                        max_tile_bytes=512*512*4*(len(plan.nodes)+3),check_active=check_active)
            protected=mask(params.protected_mask)
            generated=mask(result.generated_mask,'output')
            validate_generated_coverage(generated,effective,opacity=params.opacity,blend=params.blend,
                source=mask(parent.generated_mask) if parent else None,proposal=mask(proposal_parent.generated_mask),
                protected=protected,check_active=check_active)
            source_alpha=arrays.alpha(_ref(source.alpha) if source.alpha else None,shape,mode=alpha_mode)
            proposal_alpha=arrays.alpha(_ref(proposal.alpha) if proposal.alpha else None,shape,mode=alpha_mode)
            if source.alpha is not None and alpha_mode=='normalize_uint16':
                import numpy as np
                original=arrays.array(_ref(source.alpha),shape,source='input',unit=True,legacy_alpha=True)
                if original.dtype==np.uint16:
                    if (output.alpha is None or output.alpha==source.alpha or result.working_image==params.base.source
                            or reader.evidence(_ref(output.alpha)).source!='output'
                            or reader.evidence(_ref(output.alpha)).role!='alpha'
                            or reader.evidence(_ref(result.working_image)).source!='output'):
                        raise AdoptionInvalid('LAYER_ADOPTION_ALPHA_REFERENCE_INVALID')
            validate_composed_pixels(array(output.pixels,(*shape,3)),mask(result.changed_mask,'output'),
                array(source.pixels,(*shape,3)),array(proposal.pixels,(*shape,3)),effective,
                color=source.color,opacity=params.opacity,blend=params.blend,protected=protected,
                output_alpha=array(output.alpha,unit=True),source_alpha=source_alpha,
                proposal_alpha=proposal_alpha,check_active=check_active)
    except AdoptionInvalid:
        raise
    except ValueError as error:
        raise AdoptionInvalid('LAYER_ADOPTION_COMPOSITION_INVALID') from error
