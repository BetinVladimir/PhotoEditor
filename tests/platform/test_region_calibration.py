from copy import deepcopy
from hashlib import sha256
from types import SimpleNamespace

import pytest

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.operations.region.calibration import PinnedRegionCalibrationCatalog, region_workload_sha
from photo_worker.operations.region.resources import region_source_resources
from photo_worker.runtime.executor import OperationFailure
from test_region_preview_presentation import fixture


def calibrated(alpha=False,runtime_sha='a'*64):
    params,body=fixture(alpha)
    source=params.validate_source(body)
    q=dict(runtime_lock_sha=runtime_sha,model_lock_sha=params.frozen_recipe.model_lock_sha,
        icc_lock_sha=params.frozen_recipe.icc_lock_sha,device_profile_sha='b'*64,
        dataset_sha='c'*64,policy_sha='d'*64,backend='cuda')
    peak=dict(ram_bytes=100,scratch_bytes=200,vram_bytes=300,output_bytes=400)
    sample=dict(width=400,height=300,operator_peak=peak)
    calibration=dict(qualification=q,workload_sha=region_workload_sha(params.frozen_recipe),
        evidence_sha='e'*64,samples=[sample,dict(width=800,height=600,operator_peak={**peak,'vram_bytes':600})])
    raw=canonical_bytes(dict(schema_version=1,profiles=[calibration]))
    pin=dict(qualification=q,catalog_sha=sha256(raw).hexdigest(),calibration_sha=canonical_hash(calibration),
        workload_sha=calibration['workload_sha'],evidence_sha=calibration['evidence_sha'],sample=sample)
    src=region_source_resources(400,300,has_alpha=alpha,alpha_mode='normalize_uint16')
    pixels=120000;roi=params.geometry.output_width*params.geometry.output_height
    profile=dict(backend='cuda',renderer_version='v2.2',region_runtime_sha256=runtime_sha,
        model_lock_sha=q['model_lock_sha'],icc_lock_sha=q['icc_lock_sha'],alpha_mode='normalize_uint16',
        region_qualification=pin,resource_estimate=dict(pixel_count=pixels,
            ram_bytes=max(3*1024**3,src['ram_bytes']+16*pixels+64*roi)+100,
            scratch_bytes=src['scratch_bytes']+16*pixels+5*roi+26*1024**2+200,
            vram_bytes=300,output_bytes=24*1024**2+400))
    return raw,profile,params,source


@pytest.mark.parametrize('alpha',[False,True])
def test_installed_catalog_independently_accepts_complete_budget(alpha):
    raw,profile,params,source=calibrated(alpha)
    catalog=PinnedRegionCalibrationCatalog(raw,sha256(raw).hexdigest())
    catalog.admit(profile,params)
    catalog.admit(profile,params,source)


@pytest.mark.parametrize('field',['catalog_sha','calibration_sha','workload_sha','evidence_sha'])
def test_profile_cannot_replace_independent_pins(field):
    raw,profile,params,source=calibrated()
    catalog=PinnedRegionCalibrationCatalog(raw,sha256(raw).hexdigest())
    profile['region_qualification'][field]='0'*64
    with pytest.raises(OperationFailure,match='REGION_CALIBRATION_UNAVAILABLE'):
        catalog.admit(profile,params,source)


@pytest.mark.parametrize('field',['ram_bytes','scratch_bytes','vram_bytes','output_bytes','pixel_count'])
def test_measured_resources_cannot_be_replaced(field):
    raw,profile,params,source=calibrated()
    catalog=PinnedRegionCalibrationCatalog(raw,sha256(raw).hexdigest())
    profile['resource_estimate'][field]+=1
    with pytest.raises(OperationFailure,match='REGION_CALIBRATION_UNAVAILABLE'):
        catalog.admit(profile,params,source)


def test_covering_sample_selection_is_independent_of_task_claim():
    raw,profile,params,source=calibrated()
    catalog=PinnedRegionCalibrationCatalog(raw,sha256(raw).hexdigest())
    import json
    profile['region_qualification']['sample']=json.loads(raw)['profiles'][0]['samples'][1]
    profile['resource_estimate']['vram_bytes']=600
    with pytest.raises(OperationFailure,match='REGION_CALIBRATION_UNAVAILABLE'):
        catalog.admit(profile,params,source)


@pytest.mark.parametrize('change',[lambda p:p['profiles'][0]['samples'][0]['operator_peak'].pop('ram_bytes'),
    lambda p:p['profiles'][0]['samples'][0]['operator_peak'].update(vram_bytes=True),
    lambda p:p['profiles'].append(deepcopy(p['profiles'][0])),
    lambda p:p['profiles'][0]['samples'].append(deepcopy(p['profiles'][0]['samples'][0]))])
def test_catalog_rejects_unmeasured_or_ambiguous_fields(change):
    import json
    raw,*_=calibrated();value=json.loads(raw);change(value);raw=canonical_bytes(value)
    with pytest.raises(ValueError):
        PinnedRegionCalibrationCatalog(raw,sha256(raw).hexdigest())


def test_file_catalog_rechecks_installed_bytes_and_rejects_links(tmp_path):
    raw,profile,params,source=calibrated()
    path=tmp_path/'catalog.json';path.write_bytes(raw)
    catalog=PinnedRegionCalibrationCatalog.load(path,sha256(raw).hexdigest())
    catalog.admit(profile,params,source)
    path.write_bytes(b'{}')
    with pytest.raises(OperationFailure,match='REGION_CALIBRATION_PIN_MISMATCH'):
        catalog.admit(profile,params,source)
    link=tmp_path/'link';link.symlink_to(path)
    with pytest.raises(OSError):PinnedRegionCalibrationCatalog.load(link,sha256(raw).hexdigest())


def test_pinned_renderer_checks_catalog_before_provider_and_input_io(tmp_path):
    from test_region_pinned_renderer import provider
    from photo_worker.operations.region.renderer import PinnedRegionRenderer
    delegate,path,events=provider(tmp_path)
    raw,profile,params,source=calibrated(runtime_sha=delegate.runtime_sha256)
    delegate.device_profile_sha=profile["region_qualification"]["qualification"]["device_profile_sha"]
    delegate.model_lock_sha=params.frozen_recipe.model_lock_sha
    delegate.icc_lock_sha=params.frozen_recipe.icc_lock_sha
    catalog=PinnedRegionCalibrationCatalog(raw,sha256(raw).hexdigest())
    installed=PinnedRegionRenderer(delegate,path,delegate.runtime_sha256,calibration_catalog=catalog)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile=profile))
    installed.admit_resources(ctx,params)
    with installed.render(ctx,None,params,source,None):pass
    assert events[-2:]==['render','release']
    profile['region_qualification']['calibration_sha']='0'*64
    before=list(events)
    with pytest.raises(OperationFailure,match='REGION_CALIBRATION_UNAVAILABLE'):
        installed.admit_resources(ctx,params)
    assert events==before+['qualify']


@pytest.mark.parametrize('version',[True,1.0,'1',None])
def test_manifest_version_is_an_exact_integer(version):
    import json
    raw,*_=calibrated();value=json.loads(raw);value['schema_version']=version
    raw=canonical_bytes(value)
    # JCS serializes1.0 as1; canonical input therefore represents integer1.
    if version is not True and version==1.0:
        PinnedRegionCalibrationCatalog(raw,sha256(raw).hexdigest())
    else:
        with pytest.raises(ValueError):
            PinnedRegionCalibrationCatalog(raw,sha256(raw).hexdigest())


def test_calibrated_renderer_rejects_another_qualified_device(tmp_path):
    from test_region_pinned_renderer import provider
    from photo_worker.operations.region.renderer import PinnedRegionRenderer
    delegate,path,events=provider(tmp_path)
    raw,profile,params,source=calibrated(runtime_sha=delegate.runtime_sha256)
    delegate.device_profile_sha='f'*64
    delegate.model_lock_sha=params.frozen_recipe.model_lock_sha
    delegate.icc_lock_sha=params.frozen_recipe.icc_lock_sha
    catalog=PinnedRegionCalibrationCatalog(raw,sha256(raw).hexdigest())
    installed=PinnedRegionRenderer(delegate,path,delegate.runtime_sha256,calibration_catalog=catalog)
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile=profile))
    with pytest.raises(OperationFailure,match='REGION_CALIBRATION_UNAVAILABLE'):
        installed.admit_resources(ctx,params)
    assert events==['qualify','qualify']
    delegate.device_profile_sha='b'*64
    with pytest.raises(OperationFailure,match='RUNTIME_LOCK_MISMATCH'):
        installed.verify_runtime()
