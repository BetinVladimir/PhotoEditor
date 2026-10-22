"""Version-pinned report reads with synthetic authority; no real SQL issuer proof."""
from copy import deepcopy
from dataclasses import asdict
from hashlib import sha256
from io import BytesIO
import json
from pathlib import Path
from types import SimpleNamespace
from uuid import UUID,uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.images import TransformChain
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.retouch_qualification_loader import retouch_qualification_loader
from test_retouch_qualification_report import retouch_fixture


def setup(transport_only=False):
    policy,_,report_raw,pin=retouch_fixture()
    root=Path(__file__).resolve().parents[2]
    params=json.loads((root/'backend/internal/semanticreview/testdata/retouch_preview_params.json').read_text())
    params.update(model_lock_sha=pin.model_lock_sha,runtime_lock_sha=pin.runtime_lock_sha,device_profile_sha=pin.device_profile_sha)
    def ref():return dict(artifact_id=str(uuid4()),sha256='a'*64)
    manual=dict(artifact=ref(),canvas=dict(width=8,height=8,orientation=1,space_id='working',source_sha256='d'*64),
        transform_chain=TransformChain.create([]).model_dump(mode='json'),object_id=None,selector='manual',confidence=.9,model_lock_id='a'*64,protected=False)
    context=dict(schema_version=1,algorithm='beautyfilter_v1',source=params['source'],candidate=params['candidate'],transferred_mask=ref(),
        protection=params['protection'],faces=[dict(landmarks=ref(),object_id=None,skin=None,manual=manual,manual_mode=True)],
        beauty=dict(enabled=True,algorithm='beautyfilter_v1',smoothing=.5,tone_evenness=.2,texture_keep=.8,skin_mask='face68_conservative_v1',protect_features=True),
        **{key:params[key] for key in ('model_lock_sha','runtime_lock_sha','device_profile_sha')})
    body=canonical_bytes(context);context_ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(body).hexdigest())
    lease=SimpleNamespace(command=SimpleNamespace(operation='semantic.preview_retouch_region',operation_version='1',tenant_id=UUID(params['tenant_id']),
        params=ArtifactRef(artifact_id=uuid4(),sha256=sha256(canonical_bytes(params)).hexdigest()),inputs=[context_ref]),fence=7)
    record=dict(pin=json.loads(json.dumps(asdict(pin),default=str)),policy=policy,report=dict(artifact_id=str(pin.report.artifact_id),sha256=pin.report.sha256,
        bucket='owned',object_key='report',object_version='exact-v1',bytes=len(report_raw),media_type='application/json'))
    index_inputs=tuple(lease.command.inputs)
    if transport_only:lease.command.inputs=[]
    calls=[]
    class Database:
        after=None
        def retouch_preview_report_evidence(self,owner,raw,ref,supplied_body):
            assert owner.fence==7 and raw==canonical_bytes(params) and ref==context_ref and supplied_body==body
            owner.fence=99;calls.append('authority')
            return self.after if self.after is not None and len(calls)>1 else record
    class Objects:
        response=None
        def get_object(self,bucket,key,*,version_id):
            assert (bucket,key,version_id)==('owned','report','exact-v1')
            self.response=BytesIO(report_raw);return self.response
    db,objects=Database(),Objects()
    def load():return retouch_qualification_loader(db,lease,objects,'owned',params=params,context_ref=context_ref,
        context_raw=body,frozen_pin=pin,check_active=lambda:None,declared_inputs=index_inputs if transport_only else tuple(lease.command.inputs))()
    return db,objects,record,lease,context_ref,calls,load


def test_retouch_qualification_owns_scope_and_rechecks_exact_object_version():
    _,objects,record,lease,_,calls,load=setup()
    assert load()==record['pin'] and calls==['authority','authority'] and objects.response.closed
    assert lease.fence==7


@pytest.mark.parametrize('failure',['empty_version','null_version','wrong_bucket','size_bool','params_sha','undeclared_context','foreign_operation','late_authority'])
def test_retouch_qualification_refuses_changed_scope_or_object(failure):
    db,objects,record,lease,context_ref,calls,load=setup()
    if failure=='empty_version':record['report']['object_version']=''
    if failure=='null_version':record['report']['object_version']='null'
    if failure=='wrong_bucket':record['report']['bucket']='foreign'
    if failure=='size_bool':record['report']['bytes']=True
    if failure=='params_sha':lease.command.params.sha256='0'*64
    if failure=='undeclared_context':lease.command.inputs=[]
    if failure=='foreign_operation':lease.command.operation='semantic.preview_background_cleanup'
    if failure=='late_authority':
        db.after=deepcopy(record);db.after['policy']['required_checks']=['changed']
    with pytest.raises(AdoptionInvalid):load()
    if failure=='late_authority':assert objects.response.closed and len(calls)==2
    else:assert objects.response is None


def test_qualification_uses_full_index_without_replacing_transport_command():
    _,_,record,lease,_,calls,load=setup(transport_only=True)
    assert load()==record['pin'] and calls==['authority','authority']
    assert lease.command.inputs==[]
