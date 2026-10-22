"""Accepted authored masks and series corrections for native color replay."""
from contextlib import contextmanager,ExitStack
from types import SimpleNamespace
import numpy as np
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.image_local import LocalParams
from photo_worker.contracts.series import SeriesCorrections
from photo_worker.runtime.arrays import read_array


def local_evidence(effective,verified):
    required=bool(effective.extensions.region_adjustments) or (effective.extensions.series is not None and effective.extensions.series.enabled)
    entries=[entry for entry in verified if entry[0].operation=='image.local']
    if not required:return None
    if len(entries)!=1:raise ValueError('REGION_LOCAL_STAGE_COVERAGE_MISMATCH')
    stage,raw,source,_,policy=entries[0]
    params=LocalParams.model_validate_json(canonical_bytes(raw));resolved=policy.get('resolved')
    if (resolved is None or resolved.strength!=effective.strength
            or resolved.modules.extensions.region_adjustments!=effective.extensions.region_adjustments
            or resolved.modules.extensions.series!=effective.extensions.series):
        raise ValueError('REGION_LOCAL_EFFECTIVE_POLICY_MISMATCH')
    rules={rule.region_id:rule for rule in effective.extensions.region_adjustments}
    if {region.region_id for region in params.regions}!=set(rules):
        raise ValueError('REGION_LOCAL_MASK_COVERAGE_MISMATCH')
    for region in params.regions:
        rule=rules[region.region_id];mask=region.mask
        expected='person' if region.invert else rule.selector
        if (region.invert and rule.selector!='background' or mask.selector!=expected or mask.protected
                or mask.confidence<.75 or mask.canvas!=source.canvas or mask.transform_chain!=source.transform_chain):
            raise ValueError('REGION_LOCAL_MASK_POLICY_MISMATCH')
    series=effective.extensions.series
    if (params.series is not None)!=(series is not None and series.enabled) or (params.series is None)!=(params.item_id is None):
        raise ValueError('REGION_LOCAL_SERIES_BINDING_INVALID')
    return params,source


def series_correction(effective,params,document):
    if params.series is None:return None
    raw=document(params.series);value=SeriesCorrections.model_validate_json(raw)
    if canonical_bytes(value)!=raw or canonical_hash(value)!=params.series.sha256:
        raise ValueError('REGION_SERIES_DOCUMENT_INVALID')
    if len({item.item_id for item in value.items})!=len(value.items):raise ValueError('REGION_SERIES_DUPLICATE_ITEM')
    matches=[item for item in value.items if item.item_id==params.item_id]
    series=effective.extensions.series
    if len(matches)!=1:raise ValueError('REGION_SERIES_ITEM_REQUIRED')
    item=matches[0]
    if abs(item.applied_ev)>series.ev_limit or any(not series.rgb_gain_limits[0]<=gain<=series.rgb_gain_limits[1] for gain in item.applied_gains):
        raise ValueError('REGION_SERIES_EFFECTIVE_LIMIT_MISMATCH')
    return item


@contextmanager
def prepare_region_color_inputs(artifacts,effective,verified,metadata,document,*,check_active=lambda:None):
    entry=local_evidence(effective,verified)
    if entry is None:
        yield SimpleNamespace(region_masks={},series_correction=None,series_item_id=None);return
    params,source=entry
    if source.canvas!=metadata.canvas or source.transform_chain!=metadata.transform_chain:
        raise ValueError('REGION_LOCAL_SOURCE_GEOMETRY_MISMATCH')
    correction=series_correction(effective,params,document)
    with ExitStack() as scopes:
        masks={}
        for region in params.regions:
            check_active();artifacts.require_declared_input(region.mask.artifact)
            local=scopes.enter_context(artifacts.input(region.mask.artifact,suffix='.npy'))
            mask=scopes.enter_context(read_array(local.path,(source.canvas.height,source.canvas.width),'float32',check_active,unit_interval=True))
            if region.invert:
                owner=scopes.enter_context(artifacts.scratch.reserve(mask.size*4+8192));path=owner.path('region-inverted-mask','.npy')
                mapped=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=mask.shape)
                try:
                    for y in range(0,mask.shape[0],256):check_active();np.subtract(np.float32(1),mask[y:y+256],out=mapped[y:y+256])
                    mapped.flush();owner.validate_file(path)
                finally:mapped._mmap.close()
                mask=scopes.enter_context(read_array(path,mask.shape,'float32',check_active,unit_interval=True))
            masks[region.region_id]=mask
        yield SimpleNamespace(region_masks=masks,series_correction=correction,series_item_id=params.item_id)
        check_active()
