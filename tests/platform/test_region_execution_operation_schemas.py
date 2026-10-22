"""Authored operation-schema fences; execution is intentionally deferred."""
from uuid import uuid4
import pytest
from pydantic import ValidationError
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.region.execution import execution_params_model


def reference():
    return {'artifact_id':str(uuid4()),'sha256':'a'*64}


def test_color_accepts_original_contract_and_rejects_unknown_fields():
    model=execution_params_model('image.color')
    value={'image':reference(),'resolved':reference()}
    assert model.model_validate_json(canonical_bytes(value)).image.sha256=='a'*64
    with pytest.raises(ValidationError):
        model.model_validate_json(canonical_bytes(dict(value,unapproved_policy=True)))


def test_background_contract_does_not_accept_solid_without_color():
    model=execution_params_model('background.compose')
    with pytest.raises(ValidationError):
        model.model_validate({'image':reference(),'mode':'solid'})


def test_non_image_series_operation_cannot_masquerade_as_image_stage():
    with pytest.raises(ValueError,match='REGION_EXECUTION_OPERATION_SCHEMA_UNAVAILABLE'):
        execution_params_model('series.balance')


@pytest.mark.parametrize('operation',[
    'geometry.body','geometry.crop','image.denoise','image.upscale',
    'image.color','image.local','portrait.beauty','portrait.detail',
    'background.compose','background.blur','defects.repair','defects.auto_repair',
    'sensor.dust_repair','sensor.dust_auto_repair','image.inpaint','image.generate','image.outpaint',
])
def test_each_image_stage_has_an_explicit_original_contract(operation):
    assert execution_params_model(operation).model_fields['image'] is not None


def test_actual_execution_documents_validate_uuid_strings_in_json_domain(tmp_path):
    from test_image_color_adapter import Artifacts
    from photo_worker.operations.region.execution import RegionExecutionDescriptor,RegionGeometryStage,verify_execution_documents
    from photo_worker.contracts.base import canonical_hash
    a=Artifacts(tmp_path,False)
    params=a.add(a.raw);target=a.add(a.image)
    stage=RegionGeometryStage(operation='image.color',operation_version=1,task_id=uuid4(),result_id=uuid4(),attempt=1,fencing_token=1,
        params=params,source=a.image_ref,output=target,source_canvas=a.image.canvas,target_canvas=a.image.canvas,target_has_alpha=True)
    descriptor=RegionExecutionDescriptor(schema_version=1,source=a.image_ref,source_canvas_hash=canonical_hash(a.image.canvas),
        style=a.resolved.params.style,stages=[stage])
    reads=[]
    def document(ref):
        reads.append(ref.artifact_id)
        return canonical_bytes(a.contracts[ref.artifact_id])
    verified=verify_execution_documents(descriptor,a.image,a.resolved.params.style,document)
    assert verified[0][1]['image']['artifact_id']==str(a.image_ref.artifact_id)
    assert verified[0][4]['resolved']==a.resolved
    assert params.artifact_id in reads and target.artifact_id in reads


@pytest.mark.parametrize('field,value',[('scale','2'),('scale',True),('tile','256'),('tile',True)])
def test_json_domain_validation_does_not_coerce_numeric_fields(field,value):
    model=execution_params_model('image.upscale')
    params=dict(image=reference(),model_lock_id='b'*64,model_id='realesrgan_x2',scale=2)
    params[field]=value
    with pytest.raises(ValidationError):model.model_validate_json(canonical_bytes(params))
