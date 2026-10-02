"""Region plan must agree with byte-bound source and recomputed crop geometry."""
import copy
import json
from pathlib import Path
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.region.contracts import RegionPreviewParams


def region_fixture():
    root=Path(__file__).resolve().parents[2]
    image=json.loads((root/'backend/internal/review/testdata/working-images-python.json').read_text())[0]
    ref=lambda digest:dict(artifact_id=str(uuid4()),sha256=digest)
    source=ref(canonical_hash(image))
    version=str(uuid4())
    style=ref('c'*64)
    recipe=dict(schema_version=1,source=dict(asset_version_id=version,source=source,canvas_hash=canonical_hash(image['canvas'])),base_style=style,effective_delta=dict(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0),renderer_version='v2.2',icc_lock_sha='d'*64,model_lock_sha='e'*64)
    recipe_ref=ref(canonical_hash(recipe))
    pin=lambda value,key:dict(ref=value,bucket='private',object_key=key,object_version='pinned',bytes=500)
    entries=[dict(ref=source,contract='WorkingImage'),dict(ref=recipe_ref,contract='FrozenRecipe'),dict(ref=style,contract='StyleV2'),dict(ref=copy.deepcopy(image['pixels']),contract='NpyArray')]
    entries.sort(key=lambda item:item['ref']['artifact_id'])
    plan=dict(schema_version=1,tenant_id=str(uuid4()),project_id=str(uuid4()),asset_id=str(uuid4()),request=dict(version_id=version,rect=dict(x0=0,y0=0,x1=1,y1=1),recipe=recipe_ref,max_edge=1024),source=pin(source,'source'),recipe=pin(recipe_ref,'recipe'),frozen_recipe=recipe,geometry=dict(left=0,top=0,width=400,height=300,output_width=400,output_height=300),render_order='full_canvas_then_crop_then_downscale',inputs=entries)
    return plan,image


def test_region_contract_preserves_no_upscale_and_requires_exact_geometry():
    plan,image=region_fixture()
    params=RegionPreviewParams.model_validate_json(canonical_bytes(plan))
    params.validate_source(canonical_bytes(image))
    changed=copy.deepcopy(plan);changed['geometry']['output_width']=399
    params=RegionPreviewParams.model_validate_json(canonical_bytes(changed))
    with pytest.raises(ValueError,match='GEOMETRY'):params.validate_source(canonical_bytes(image))


@pytest.mark.parametrize('change',[
    lambda p:p['request']['rect'].update(x0=1),
    lambda p:p['source'].update(object_version='null'),
    lambda p:p['frozen_recipe'].update(model_lock_sha='f'*64),
    lambda p:p['source'].update(bucket='other'),
    lambda p:p.update(render_order='crop_then_render'),
    lambda p:p['inputs'].reverse(),
    lambda p:p['inputs'].append(p['inputs'][0]),
    lambda p:p.update(tenant_id='00000000-0000-0000-0000-000000000000'),
    lambda p:p['request'].update(max_edge=1025),
])
def test_region_contract_rejects_unbound_or_ambiguous_plan(change):
    plan,_=region_fixture();change(plan)
    with pytest.raises(ValueError):RegionPreviewParams.model_validate_json(canonical_bytes(plan))


def test_region_source_hash_and_required_pixel_ref_are_checked():
    plan,image=region_fixture()
    image['pixels']['sha256']='f'*64
    params=RegionPreviewParams.model_validate_json(canonical_bytes(plan))
    with pytest.raises(ValueError,match='SOURCE'):params.validate_source(canonical_bytes(image))
    plan,image=region_fixture()
    for entry in plan['inputs']:
        if entry['contract']=='NpyArray':entry['ref']['sha256']='f'*64
    params=RegionPreviewParams.model_validate_json(canonical_bytes(plan))
    with pytest.raises(ValueError,match='INPUTS'):params.validate_source(canonical_bytes(image))


@pytest.mark.parametrize('transform',[
    lambda body:body+b' ',
    lambda body:body.replace(b'"alpha":null',b'"alpha":null,"alpha":null'),
])
def test_region_source_requires_canonical_unambiguous_bytes(transform):
    plan,image=region_fixture()
    params=RegionPreviewParams.model_validate_json(canonical_bytes(plan))
    with pytest.raises(ValueError):params.validate_source(transform(canonical_bytes(image)))
