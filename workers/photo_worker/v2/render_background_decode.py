"""Pinned imported background normalization through the existing ICC decoder."""
from contextlib import contextmanager
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.assets import DecodeParams
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.asset.verify import verify_size,verify_magic,verify_suffix,VerifiedSource
from photo_worker.operations.asset.inspect import read_metadata
from photo_worker.operations.asset.decode import icc_to_work,WorkingImage
from photo_worker.runtime.arrays import read_array


@dataclass(frozen=True)
class NormalizedBackground:
    image: WorkingImage
    rgb: np.ndarray


def background_decode_peak(width,height,source_bytes,*,tile_size=512):
    """Conservative CPU decoder bound; physical runtime qualification is separate."""
    if (any(type(v) is not int or v<1 for v in (width,height,source_bytes))
            or width*height>200_000_000 or source_bytes>536870912
            or type(tile_size) is not int or tile_size not in (256,512,1024)):
        raise ValueError('RENDER_BACKGROUND_DECODE_RESOURCES_INVALID')
    pixels=width*height
    return dict(ram_bytes=12*pixels+source_bytes+1024*1024*1024+128*tile_size*tile_size,
                scratch_bytes=source_bytes+12*pixels+1048576)


def _admit_decode(profile,retained,required):
    from photo_worker.runtime.executor import OperationFailure
    keys={'ram_bytes','scratch_bytes'}
    estimate=profile.get('resource_estimate') if isinstance(profile,dict) else None
    if (not isinstance(estimate,dict) or not isinstance(retained,dict) or set(retained)!=keys
            or any(type(d.get(k)) is not int or not 0<=d[k]<=2**53-1
                   for d in (estimate,retained) for k in keys)
            or any(required[k]+retained[k]>2**53-1 for k in keys)):
        raise OperationFailure('CONFIG_INVALID')
    for key,code in (('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED')):
        if estimate[key]<retained[key]+required[key]:raise OperationFailure(code)


@contextmanager
def normalize_background_asset(artifacts,params,registry,*,execution_profile,retained_resources,check_active=lambda:None):
    """Caller binds portable ID, admits decoder resources and qualifies ICC runtime.

    Uses declared immutable TaskArtifacts input and the existing libvips/LCMS
    tiled decode, including orientation and bit-depth preservation. Alpha
    backgrounds are rejected rather than silently flattened. Missing input ICC
    requires an explicit assumed profile in pinned DecodeParams.
    """
    check_active()
    # Snapshot budgets before callbacks or IO can change caller dictionaries.
    from copy import deepcopy
    execution_profile=deepcopy(execution_profile);retained_resources=deepcopy(retained_resources)
    _admit_decode(execution_profile,retained_resources,dict(ram_bytes=0,scratch_bytes=0))
    if not isinstance(params,DecodeParams):
        raise ValueError('RENDER_BACKGROUND_DECODE_PARAMS_INVALID')
    params=DecodeParams.model_validate_json(canonical_bytes(params))
    if params.analyze:
        raise ValueError('RENDER_BACKGROUND_DECODE_ANALYZE_INVALID')
    _admit_decode(execution_profile,retained_resources,
        dict(ram_bytes=params.expected_bytes,scratch_bytes=params.expected_bytes))
    with artifacts.input(params.source,suffix=params.suffix) as local:
        size,digest=verify_size(local.path,params.expected_bytes,params.source.sha256,
            check_active=check_active)
        kind=verify_magic(local.path);verify_suffix(kind,params.suffix)
        if kind not in ('jpeg','png','tiff'):
            raise ValueError('RENDER_BACKGROUND_FORMAT_INVALID')
        source=VerifiedSource(local.path,kind,size,digest)
        metadata=read_metadata(source)
        if metadata.has_alpha:
            raise ValueError('RENDER_BACKGROUND_ALPHA_UNSUPPORTED')
        required=background_decode_peak(metadata.width,metadata.height,params.expected_bytes,tile_size=params.tile)
        _admit_decode(execution_profile,retained_resources,required)
        check_active()
        with artifacts.scratch.reserve(metadata.width*metadata.height*12+1048576) as reservation:
            path=reservation.planned_path('render-background-normalized','.npy')
            decoded=icc_to_work(source,path,registry,params.assume_profile,tile=params.tile,
                check_active=check_active)
            reservation.validate()
            with read_array(decoded.rgb_path,(decoded.height,decoded.width,3),'float32',
                    check_active,unit_interval=True) as rgb:
                check_active()
                yield NormalizedBackground(decoded,rgb)
                check_active()


@contextmanager
def normalize_frozen_background(artifacts,binding,registry,*,execution_profile,retained_resources,tile_size=512,
                                check_active=lambda:None):
    """Require exact frozen target ICC before importing pixels."""
    from .render_background_binding import FrozenBackgroundAsset,decode_frozen_background
    if not isinstance(binding,FrozenBackgroundAsset):
        raise ValueError('RENDER_BACKGROUND_BINDING_INVALID')
    binding=decode_frozen_background(canonical_bytes(binding))
    check_active()
    if registry.resolve('work_rec2020_gamma24_v1').sha256!=binding.work_icc_sha:
        raise ValueError('RENDER_BACKGROUND_ICC_PIN_CHANGED')
    with normalize_background_asset(artifacts,binding.decode_params(tile=tile_size),registry,
            execution_profile=execution_profile,retained_resources=retained_resources,check_active=check_active) as result:
        if result.image.profile_sha256!=binding.work_icc_sha:
            raise ValueError('RENDER_BACKGROUND_ICC_PIN_CHANGED')
        yield result
        check_active()
