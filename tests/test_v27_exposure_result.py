from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.semantic.exposure_result import publish_exposure_result
from test_v27_exposure_source import source_fixture


@pytest.mark.parametrize('status', ['pass','review','fail','unavailable'])
def test_report_required_before_image_result_publication(status):
    edit,_,_=source_fixture();roles=[]
    staged=SimpleNamespace(image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),
        pixels=ArtifactRef(artifact_id=uuid4(),sha256='b'*64),bytes_written=100)
    report=dict(schema_version=1,status=status,operation='semantic.local_exposure',
        skipped_count=0,reason=None,outside_mask_max_error=0.,clipping_fraction=0.,
        identity=None,transform=None,sources=[edit['source']],registrations=[])
    def stage(value,role):
        roles.append(role)
        if role=='document':
            assert value.working_image==staged.image and value.generated_mask is None
        return ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
    options=dict(verify_report=lambda *args:True,verify_admission=lambda edit:True,
        check_active=lambda:None,max_output_bytes=1048576)
    artifacts=SimpleNamespace(stage_contract=stage)
    if status in ('fail','unavailable'):
        with pytest.raises(ValueError,match='REPORT_INVALID'):
            publish_exposure_result(artifacts,edit,staged,report,**options)
        assert roles==[]
    else:
        result=publish_exposure_result(artifacts,edit,staged,report,**options)
        assert roles==['report','document'] and result.review_required==(status=='review')
