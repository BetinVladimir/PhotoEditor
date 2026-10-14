"""V29 source-to-encoded multi-image preparation, still before publication."""
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.session.contracts import VersionBinding
from .multi_sources import prepare_multi_sources, multi_source_resources
from .multi_prepare import prepare_automatic_planar_stack, multi_planar_resources, multi_proxy_ram, AutomaticMultiPrepared, _motion_parameters
from .cylindrical_stack import prepare_cylindrical_panorama, cylindrical_projection_resources
from .multi_encoding import prepare_multi_encoding, EncodedMultiFiles


@dataclass(frozen=True)
class PreparedMultiImage:
    images: tuple[WorkingImage, ...]
    bindings: tuple[VersionBinding, ...]
    automatic: AutomaticMultiPrepared
    projections: tuple
    encoded: EncodedMultiFiles
    geometry_changed: bool


class RemainingScratch:
    def __init__(self,parent,retained):
        self.parent=parent;self.budget_bytes=parent.budget_bytes-retained
        if self.budget_bytes<0:raise ValueError('MULTI_SCRATCH_EXCEEDED')
    def reserve(self,size):return self.parent.reserve(size)


@contextmanager
def prepare_multi_image(images,bindings,rgbs,alphas,reference,scratch,*,operation,
        projection='planar',max_pixels,ram_max_bytes,verify_icc,registration_error_px,
        match_inlier_min,motion_threshold,motion_policy='review',tile=256,check_active=lambda:None):
    if (operation not in ('panorama','focus_stack') or projection not in ('planar','cylindrical')
            or operation=='focus_stack' and projection!='planar'
            or not isinstance(images,(list,tuple)) or not 2<=len(images)<=30
            or operation=='panorama' and len(images)>20
            or not isinstance(bindings,(list,tuple)) or len(bindings)!=len(images)
            or type(reference) is not int or not 0<=reference<len(images)
            or type(max_pixels) is not int or not 1<=max_pixels<=200000000):
        raise ValueError('MULTI_IMAGE_INVALID')
    _motion_parameters(operation, motion_threshold, motion_policy)
    owned=tuple(WorkingImage.model_validate_json(canonical_bytes(i)) for i in images)
    scoped=tuple(VersionBinding.model_validate_json(canonical_bytes(b)) for b in bindings)
    seen=set()
    for image,binding in zip(owned,scoped):
        if (binding.source.sha256!=canonical_hash(image) or binding.canvas_hash!=canonical_hash(image.canvas)
                or binding.asset_version_id in seen):raise ValueError('MULTI_SOURCE_MISMATCH')
        seen.add(binding.asset_version_id)
    shapes=[(i.canvas.height,i.canvas.width) for i in owned]
    source_resources=multi_source_resources(shapes,tile=tile)
    # Full tenant/object/snapshot authority must be established independently.
    with ExitStack() as lifetime:
        sources=lifetime.enter_context(prepare_multi_sources(owned,rgbs,alphas,scratch,
            verify_icc=verify_icc,ram_max_bytes=ram_max_bytes,tile=tile,check_active=check_active))
        remaining_ram=ram_max_bytes-source_resources['ram_bytes']
        source_scratch=RemainingScratch(scratch,source_resources['scratch_bytes'])
        kwargs=dict(max_pixels=max_pixels,ram_max_bytes=remaining_ram,registration_error_px=registration_error_px,
            match_inlier_min=match_inlier_min,motion_threshold=motion_threshold,luma=sources.luma,
            tile=tile,check_active=check_active)
        projections=();projection_ram=0;projection_disk=0
        if projection=='cylindrical':
            cylindrical=lifetime.enter_context(prepare_cylindrical_panorama(sources.rgb,sources.alpha,reference,source_scratch,**kwargs))
            automatic=cylindrical.panorama;projections=cylindrical.projections
            costs=cylindrical_projection_resources(shapes,tile=tile)
            projection_ram=costs['ram_bytes'];projection_disk=costs['scratch_bytes']
        else:
            automatic=lifetime.enter_context(prepare_automatic_planar_stack(sources.rgb,sources.alpha,reference,
                source_scratch,operation=operation,motion_policy=motion_policy,**kwargs))
        prepared=automatic.prepared
        live=multi_planar_resources(shapes,prepared.shape,operation,tile=tile)
        encoding_ram=remaining_ram-projection_ram-multi_proxy_ram(shapes)-live['ram_bytes']
        if encoding_ram<1:raise ValueError('MULTI_RAM_EXCEEDED')
        encoding_scratch=RemainingScratch(source_scratch,projection_disk+live['scratch_bytes'])
        def reopen(path):
            value=np.load(path,mmap_mode='r',allow_pickle=False)
            lifetime.callback(value._mmap.close);return value
        linear=reopen(prepared.result.rgb);opacity=reopen(prepared.result.alpha)
        geometry_changed=bool(projections) or prepared.shape!=shapes[reference] or not np.array_equal(
            prepared.registrations[reference].source_to_output,np.eye(3))
        encoded=lifetime.enter_context(prepare_multi_encoding(linear,opacity,rgbs[reference],sources.alpha[reference],
            sources.color,encoding_scratch,geometry_changed=geometry_changed,verify_icc=verify_icc,
            ram_max_bytes=encoding_ram,tile=tile,check_active=check_active))
        check_active()
        yield PreparedMultiImage(owned,scoped,automatic,tuple(projections),encoded,geometry_changed)
