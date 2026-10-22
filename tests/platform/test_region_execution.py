"""Authored descriptor structural scenarios; no qualification inferred."""
from uuid import uuid4
import json
import pytest
from pydantic import ValidationError
from photo_worker.contracts.base import ArtifactRef,canonical_hash,canonical_bytes
from photo_worker.contracts.images import Canvas,ResizeTransform,Rect
from photo_worker.operations.region.execution import RegionExecutionDescriptor,RegionGeometryStage


def fixture():
    def ref():return ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    source=ref();canvas=Canvas(width=10,height=8,orientation=1,space_id='work',source_sha256='a'*64)
    target=canvas.model_copy(update=dict(width=20,height=16,space_id='upscale'))
    first=RegionGeometryStage(operation='image.color',operation_version=1,task_id=uuid4(),result_id=uuid4(),attempt=1,fencing_token=1,
        params=ref(),source=source,output=ref(),source_canvas=canvas,target_canvas=canvas)
    second=RegionGeometryStage(operation='image.upscale',operation_version=1,task_id=uuid4(),result_id=uuid4(),attempt=1,fencing_token=2,
        params=ref(),source=first.output,output=ref(),source_canvas=canvas,target_canvas=target,
        transform=ResizeTransform(kind='resize',from_canvas=canvas,to_canvas=target,invertibility='invertible',
            valid_region=Rect(x=0,y=0,width=20,height=16),pixel_centers='half_pixel'))
    return RegionExecutionDescriptor(schema_version=1,source=source,style=ref(),source_canvas_hash=canonical_hash(canvas),stages=[first,second])


def test_actual_identity_stage_then_upscale_is_preserved():
    value=fixture();copied=RegionExecutionDescriptor.model_validate_json(value.model_dump_json())
    assert copied.stages[0].transform is None
    assert copied.stages[1].target_canvas.width==20


@pytest.mark.parametrize('change',['source','task','transform','canvas'])
def test_descriptor_rejects_broken_canonical_geometry_chain(change):
    value=fixture().model_dump(mode='json');stage=value['stages'][1]
    if change=='source':stage['source']=value['source']
    elif change=='task':stage['task_id']=value['stages'][0]['task_id']
    elif change=='transform':stage.pop('transform')
    else:stage['source_canvas']['width']=11
    with pytest.raises(ValidationError):RegionExecutionDescriptor.model_validate(value)


@pytest.mark.parametrize('field,value',[('operation_version',True),('attempt',1.0),('fencing_token','1')])
def test_descriptor_does_not_coerce_receipt_numbers(field,value):
    document=fixture().model_dump(mode='json');document['stages'][0][field]=value
    with pytest.raises(ValidationError):RegionExecutionDescriptor.model_validate_json(json.dumps(document))


def test_identity_operation_cannot_smuggle_geometry_transform():
    document=fixture().model_dump(mode='json')
    document['stages'][1]['operation']='image.color'
    with pytest.raises(ValidationError):RegionExecutionDescriptor.model_validate_json(json.dumps(document))


@pytest.mark.parametrize('value',[0,1,'true',None])
def test_target_alpha_receipt_does_not_coerce_non_boolean_values(value):
    document=fixture().model_dump(mode='json');document['stages'][-1]['target_has_alpha']=value
    with pytest.raises(ValidationError,match='ALPHA_BOOLEAN_REQUIRED'):
        RegionExecutionDescriptor.model_validate_json(json.dumps(document))


def test_target_alpha_is_independent_of_unchanged_canvas_geometry():
    document=fixture().model_dump(mode='json');document['stages']=document['stages'][:1]
    document['stages'][0].update(operation='background.compose',target_has_alpha=True)
    descriptor=RegionExecutionDescriptor.model_validate_json(json.dumps(document))
    assert descriptor.stages[0].target_has_alpha is True
    assert descriptor.stages[0].transform is None


def crop_descriptor():
    document=fixture().model_dump(mode='json');stage=document['stages'][1]
    stage['operation']='geometry.crop'
    stage['transform']['kind']='matrix'
    stage['transform'].pop('pixel_centers')
    stage['transform']['forward_matrix']=[2,0,0,0,2,0,0,0,1]
    stage['plan']=dict(artifact_id=str(uuid4()),sha256='b'*64)
    stage['policy_params']=dict(artifact_id=str(uuid4()),sha256='c'*64)
    return document


def test_crop_keeps_canonical_plan_and_producer_policy_refs():
    descriptor=RegionExecutionDescriptor.model_validate_json(canonical_bytes(crop_descriptor()))
    assert descriptor.stages[-1].plan!=descriptor.stages[-1].policy_params


@pytest.mark.parametrize('missing',['plan','policy_params'])
def test_crop_requires_plan_and_its_actual_producer_params(missing):
    document=crop_descriptor();document['stages'][-1].pop(missing)
    with pytest.raises(ValidationError,match='COMPOSITION_POLICY_REQUIRED'):
        RegionExecutionDescriptor.model_validate_json(json.dumps(document))
