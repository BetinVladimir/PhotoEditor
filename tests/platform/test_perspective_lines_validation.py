from dataclasses import replace
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage, Canvas, ColorSpace, TransformChain
from photo_worker.contracts.base import ArtifactRef as ContractRef
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.perspective_lines_validation import verify_perspective_lines_result


class Reader:
    def __init__(self, available=True):
        self.image=WorkingImage(pixels=ContractRef(artifact_id=uuid4(),sha256='a'*64),
            alpha=None,dtype='float32',canvas=Canvas(width=100,height=80,orientation=1,
            space_id='source',source_sha256='b'*64),color=ColorSpace(profile_id='linear_srgb_v1',
            encoding='linear',icc_sha256='c'*64),transform_chain=TransformChain.create([]))
        self.source_id=uuid4()
        self.base=VersionBinding.model_validate_json(canonical_bytes(dict(
            asset_version_id=str(uuid4()),source=dict(artifact_id=str(self.source_id),
            sha256=canonical_hash(self.image)),canvas_hash=canonical_hash(self.image.canvas))))
        self.params=dict(schema_version=1,base=self.base.model_dump(mode='json'))
        lines=dict(schema_version=1,lines=[
            dict(a=dict(x=.1,y=.1),b=dict(x=.9,y=.1),family='horizontal'),
            dict(a=dict(x=.1,y=.9),b=dict(x=.9,y=.9),family='horizontal'),
            dict(a=dict(x=.1,y=.1),b=dict(x=.1,y=.9),family='vertical'),
            dict(a=dict(x=.9,y=.1),b=dict(x=.9,y=.9),family='vertical')])
        self.result=dict(schema_version=1,base=self.base.model_dump(mode='json'),
            status='available' if available else 'unsupported',lines=lines if available else None)
        self.refresh()
    def refresh(self):
        self.docs={};self.meta={}
        for name,value,source,role in [('params',self.params,'input','task_params'),
                ('result',self.result,'output','document'),('source',self.image,'input',None)]:
            raw=canonical_bytes(value)
            ref=ArtifactRef(self.source_id if name=='source' else uuid4(),sha256(raw).hexdigest())
            setattr(self,name+'_ref',ref);self.docs[ref]=raw
            self.meta[ref]=ArtifactEvidence(ref,'application/json',len(raw),role,source)
    def evidence(self,ref):return self.meta[ref]
    def read_json(self,ref,limit):
        assert len(self.docs[ref])<=limit
        return self.docs[ref]
    def staged_outputs(self):return {self.result_ref}
    def verify(self,**kwargs):return verify_perspective_lines_result(self,self.params_ref,self.result_ref,
        expected_base=self.base,working_icc_sha='c'*64,**kwargs)


@pytest.mark.parametrize('available',[False,True])
def test_preserves_available_vs_unsupported_and_returns_owned_bytes(available):
    reader=Reader(available)
    result=reader.verify()
    assert result.base_bytes==canonical_bytes(reader.base)
    assert result.result_bytes==reader.docs[reader.result_ref]
    if available:
        assert result.lines_bytes==canonical_bytes(reader.result['lines'])
        assert result.lines_sha256==sha256(result.lines_bytes).hexdigest()
        reader.result['lines']['lines'].clear()
        assert result.lines_bytes is not None
    else: assert result.lines_bytes is None and result.lines_sha256 is None


@pytest.mark.parametrize('mutation',[
    lambda r:r.result['base'].update(asset_version_id=str(uuid4())),
    lambda r:r.params['base'].update(canvas_hash='d'*64),
    lambda r:r.result.update(status='unsupported'),
    lambda r:r.result.update(status='pass'),
    lambda r:r.result.update(extra=True),
    lambda r:r.result['lines']['lines'][1].update(a=dict(x=.1,y=.15),b=dict(x=.9,y=.15)),
])
def test_rejects_foreign_or_invalid_proposal(mutation):
    reader=Reader();mutation(reader);reader.refresh()
    with pytest.raises(AdoptionInvalid):reader.verify()


def test_rejects_foreign_icc_even_when_source_binding_is_self_consistent():
    reader=Reader();reader.image.color.icc_sha256='d'*64
    reader.base.source.sha256=canonical_hash(reader.image)
    reader.params['base']=reader.base.model_dump(mode='json')
    reader.result['base']=reader.base.model_dump(mode='json');reader.refresh()
    with pytest.raises(AdoptionInvalid):reader.verify()


@pytest.mark.parametrize('field,value',[('source','input'),('role','report'),
    ('bytes',True),('media_type','text/plain')])
def test_rejects_wrong_output_evidence(field,value):
    reader=Reader();reader.meta[reader.result_ref]=replace(reader.meta[reader.result_ref],**{field:value})
    with pytest.raises(AdoptionInvalid):reader.verify()


def test_rejects_changed_bytes_extra_outputs_and_cancellation():
    reader=Reader();reader.docs[reader.result_ref]+=b' '
    with pytest.raises(AdoptionInvalid):reader.verify()
    reader=Reader();reader.staged_outputs=lambda:{reader.result_ref,reader.params_ref}
    with pytest.raises(AdoptionInvalid):reader.verify()
    class Cancelled(Exception):pass
    def cancel():raise Cancelled()
    with pytest.raises(Cancelled):Reader().verify(check_active=cancel)


def test_rejects_oversized_candidate_set():
    reader=Reader();reader.result['lines']['lines']*=33;reader.refresh()
    with pytest.raises(AdoptionInvalid):reader.verify()


def test_rejects_noncanonical_result_even_with_matching_original_hash():
    reader=Reader();old=reader.result_ref
    raw=reader.docs[old]+b' '
    ref=ArtifactRef(old.artifact_id,sha256(raw).hexdigest())
    reader.result_ref=ref;reader.docs[ref]=raw
    reader.meta[ref]=replace(reader.meta[old],ref=ref,bytes=len(raw))
    with pytest.raises(AdoptionInvalid):reader.verify()


def test_rejects_params_source_alias_before_reading():
    reader=Reader();reader.params_ref=reader.source_ref
    reader.read_json=lambda *args:(_ for _ in ()).throw(AssertionError('must refuse before IO'))
    with pytest.raises(AdoptionInvalid):reader.verify()


def test_input_params_null_role_matches_existing_db_reader():
    reader=Reader(False)
    reader.meta[reader.params_ref]=replace(reader.meta[reader.params_ref],role=None)
    assert reader.verify().lines_bytes is None
    reader.meta[reader.params_ref]=replace(reader.meta[reader.params_ref],role='pixels')
    with pytest.raises(AdoptionInvalid):reader.verify()
