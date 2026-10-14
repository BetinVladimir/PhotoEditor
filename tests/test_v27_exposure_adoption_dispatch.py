from uuid import uuid4
import pytest
from photo_worker.v2.adoption import prepare_v2_adoption,ArtifactRef,AdoptionInvalid


def test_dispatch_cannot_skip_exposure_context():
    operations={'operations':[dict(id='semantic.local_exposure',version='1')]}
    with pytest.raises(AdoptionInvalid,match='ADOPTION_CONTEXT_REQUIRED'):
        prepare_v2_adoption(task_id=uuid4(),operation='semantic.local_exposure',version='1',
            root=ArtifactRef(uuid4(),'f'*64),reader=object(),operations=operations,
            schemas={'$defs':{'ImageResult':{}}})


@pytest.mark.parametrize('bad_report',[False,True])
def test_image_result_report_binding(monkeypatch,bad_report):
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.v2.adoption import ArtifactEvidence
    from photo_worker.v2 import exposure_adoption as module
    from test_v27_exposure_adoption import fixture
    reader,params,image=fixture()
    edit,_,_=module.verify_exposure_documents(reader,params,image,check_active=lambda:None)
    report=dict(schema_version=1,status='pass',operation='semantic.local_exposure',
        skipped_count=0,reason=None,outside_mask_max_error=1. if bad_report else 0.,
        clipping_fraction=0.,identity=None,transform=None,sources=[edit.source.model_dump(mode='json')],registrations=[])
    raw=canonical_bytes(report);report_ref=ArtifactRef(uuid4(),sha256(raw).hexdigest())
    result=dict(schema_version=1,working_image=dict(artifact_id=str(image.artifact_id),sha256=image.sha256),
        report=dict(artifact_id=str(report_ref.artifact_id),sha256=report_ref.sha256),
        changed_mask=edit.mask.model_dump(mode='json'),generated_mask=None)
    root_raw=canonical_bytes(result);root=ArtifactRef(uuid4(),sha256(root_raw).hexdigest())
    docs={root:root_raw,report_ref:raw}
    class Reader:
        def evidence(self,ref):
            if ref in docs:return ArtifactEvidence(ref,'application/json',len(docs[ref]),
                'document' if ref==root else 'report','output')
            return reader.evidence(ref)
        def read_json(self,ref,limit):return docs[ref] if ref in docs else reader.read_json(ref,limit)
    calls=[]
    monkeypatch.setattr(module,'verify_local_exposure',lambda *args,**kwargs:calls.append(True))
    if bad_report:
        with pytest.raises(AdoptionInvalid,match='REPORT_CHANGED'):
            module.verify_exposure_image_result(Reader(),params,root,check_active=lambda:None)
        assert calls==[]
    else:
        result=module.verify_exposure_image_result(Reader(),params,root,check_active=lambda:None)
        assert result.working_image.artifact_id==image.artifact_id and calls==[True]
