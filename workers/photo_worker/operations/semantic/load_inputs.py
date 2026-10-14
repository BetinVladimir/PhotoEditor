"""Pinned source/intent/protection mappings for explicitly installed V27."""
from contextlib import contextmanager,ExitStack
from copy import deepcopy
from dataclasses import dataclass
from typing import Callable
import numpy as np
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.images import MaskRef,WorkingImage
from photo_worker.contracts.semantic import EditIntent,SemanticTransferParams
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.optical.color import to_working_linear
from .resources import semantic_transfer_resources
from .transfer import SemanticTransferArrays


@dataclass(frozen=True,init=False)
class PinnedSemanticArrayLoader:
    """Protection refs and source/ICC/model authority are installer supplied."""
    _protection_documents: tuple[bytes,bytes]
    verify_inputs: Callable

    def __init__(self,reference_protection,target_protection,*,verify_inputs):
        if (not isinstance(reference_protection,ArtifactRef) or not isinstance(target_protection,ArtifactRef)
                or not callable(verify_inputs)):
            raise ValueError('SEMANTIC_INPUT_LOADER_INVALID')
        object.__setattr__(self,"_protection_documents",tuple(canonical_bytes(ref) for ref in
            (reference_protection,target_protection)))
        object.__setattr__(self,"verify_inputs",verify_inputs)

    @property
    def reference_protection(self):
        return ArtifactRef.model_validate_json(self._protection_documents[0])

    @property
    def target_protection(self):
        return ArtifactRef.model_validate_json(self._protection_documents[1])

    @contextmanager
    def __call__(self,artifacts,params,reference,target,profile,check_active):
        params,reference,target,profile=deepcopy((params,reference,target,profile))
        check_active()
        params=SemanticTransferParams.model_validate_json(canonical_bytes(params))
        reference=WorkingImage.model_validate_json(canonical_bytes(reference))
        target=WorkingImage.model_validate_json(canonical_bytes(target))
        if (canonical_hash(reference.canvas)!=params.reference.canvas_hash
                or canonical_hash(target.canvas)!=params.target.canvas_hash):
            raise ValueError('SEMANTIC_SOURCE_CHANGED')

        check_active();self.verify_inputs(deepcopy(params),deepcopy(reference),deepcopy(target));check_active()
        images=(reference,target)
        if any(image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1'
                or image.color.encoding not in ('linear','gamma24') for image in images):
            raise ValueError('SEMANTIC_COLOR_UNSUPPORTED')
        shapes=tuple((image.canvas.height,image.canvas.width) for image in images)
        required=semantic_transfer_resources(*shapes)
        counts=tuple(h*w for h,w in shapes)
        downloads=12*sum(counts)+8*counts[0]+4*counts[1]+5*8192
        conversions=sum(12*count+8192 for image,count in zip(images,counts) if image.color.encoding=='gamma24')
        required['scratch_bytes']+=downloads+conversions
        required['ram_bytes']+=conversions
        estimate=profile.get('resource_estimate') if isinstance(profile,dict) else None
        if not isinstance(estimate,dict) or any(type(estimate.get(k)) is not int or not 0<=estimate[k]<=2**53-1 for k in required):
            raise OperationFailure('CONFIG_INVALID')
        for key,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
                ('vram_bytes','VRAM_ADMISSION_EXCEEDED'),('output_bytes','OUTPUT_ADMISSION_EXCEEDED')]:
            if estimate[key]<required[key]:raise OperationFailure(code)
        def document(ref,model):
            check_active();return model.model_validate_json(canonical_bytes(artifacts.contract(ref,model)))
        intent=document(params.intent,EditIntent)
        if intent.reference!=params.reference:raise ValueError('SEMANTIC_REFERENCE_CHANGED')
        mask=document(intent.reference_mask,MaskRef)
        protections=tuple(document(ref,MaskRef) for ref in (self.reference_protection,self.target_protection))
        if mask.protected or mask.canvas!=reference.canvas or mask.transform_chain!=reference.transform_chain:
            raise ValueError('SEMANTIC_MASK_BINDING_INVALID')
        if any(not protection.protected or protection.canvas!=image.canvas
                or protection.transform_chain!=image.transform_chain for protection,image in zip(protections,images)):
            raise ValueError('SEMANTIC_PROTECTION_BINDING_INVALID')
        with ExitStack() as stack:
            def mapped(ref,shape,unit):
                local=stack.enter_context(artifacts.input(ref,suffix='.npy'))
                return stack.enter_context(read_array(local.path,shape,'float32',check_active,unit_interval=unit))
            rgb=[]
            for image,shape in zip(images,shapes):
                source=mapped(image.pixels,(*shape,3),False)
                if image.color.encoding=='linear':rgb.append(source);continue
                owner=stack.enter_context(artifacts.scratch.reserve(12*shape[0]*shape[1]+8192))
                converted=np.lib.format.open_memmap(owner.path('semantic-linear','.npy'),mode='w+',dtype='<f4',shape=(*shape,3))
                stack.callback(converted._mmap.close)
                for y in range(0,shape[0],256):
                    for x in range(0,shape[1],256):
                        check_active();part=np.s_[y:y+256,x:x+256]
                        converted[part]=to_working_linear(source[part],image.color)
                converted.flush();converted.flags.writeable=False;rgb.append(converted)
            selected=mapped(mask.artifact,shapes[0],True)
            protected=tuple(mapped(value.artifact,shape,True) for value,shape in zip(protections,shapes))
            check_active()
            self.verify_inputs(deepcopy(params),deepcopy(reference),deepcopy(target))
            check_active()
            yield SemanticTransferArrays(rgb[0],rgb[1],selected,*protected)
            self.verify_inputs(deepcopy(params),deepcopy(reference),deepcopy(target))
            check_active()


def sql_pinned_semantic_array_loader(database,lease,params_bytes,reference_protection,target_protection,
                                     *,verify_color,check_active=lambda:None):
    """Connect live source catalog checks to the existing owned array loader.

    Protection refs are trusted frozen installer pins; their origin authority
    must still be resolved before constructing this loader.
    """
    from .source_authority import semantic_input_authority
    verify=semantic_input_authority(database,lease,params_bytes,
        verify_color=verify_color,check_active=check_active)
    return PinnedSemanticArrayLoader(reference_protection,target_protection,verify_inputs=verify)


def sql_frozen_semantic_array_loader(database,lease,params_bytes,*,verify_color,verify_protection,
                                     check_active=lambda:None):
    """Resolve declared protection refs under the lease and recheck every use.

    SQL proves frozen membership and live metadata. The required protection
    verifier independently proves mask origin/coverage; it must raise on denial.
    Array/document shape and byte verification remain in the artifact loader.
    """
    from photo_worker.v2.adoption import AdoptionInvalid
    from .source_authority import semantic_input_authority
    if not callable(verify_protection) or not callable(check_active):
        raise ValueError('SEMANTIC_PROTECTION_AUTHORITY_INVALID')
    owned_lease=deepcopy(lease)
    source_verify=semantic_input_authority(database,owned_lease,params_bytes,
        verify_color=verify_color,check_active=check_active)
    def pins():
        check_active()
        value=database.semantic_protection_pins(deepcopy(owned_lease))
        if not isinstance(value,dict) or set(value)!={'source_protection','target_protection'}:
            raise AdoptionInvalid('SEMANTIC_PROTECTION_SCOPE_STALE')
        refs=tuple(ArtifactRef.model_validate_json(canonical_bytes(value[key])) for key in ('source_protection','target_protection'))
        check_active()
        return canonical_bytes(dict(zip(('source_protection','target_protection'),
            (ref.model_dump(mode='json') for ref in refs)))),refs
    frozen,refs=pins()
    def verify(params,reference,target):
        current,_=pins()
        if current!=frozen:raise AdoptionInvalid('SEMANTIC_PROTECTION_SCOPE_STALE')
        source_verify(deepcopy(params),deepcopy(reference),deepcopy(target))
        verify_protection(deepcopy(params),deepcopy(reference),deepcopy(target),*deepcopy(refs))
        check_active()
        if pins()[0]!=frozen:raise AdoptionInvalid('SEMANTIC_PROTECTION_SCOPE_STALE')
    return PinnedSemanticArrayLoader(*refs,verify_inputs=verify)
