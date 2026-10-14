from dataclasses import replace
from io import BytesIO
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.delivery_metadata_reader import (
    MetadataResultScope,MetadataObjectEvidence,DeliveryMetadataEvidenceReader)


def fixture():
    scope=MetadataResultScope(uuid4(),uuid4(),1,2,uuid4())
    refs=tuple(ArtifactRef(uuid4(),'a'*64) for _ in range(3))
    class Authority:
        values={ref:MetadataObjectEvidence(scope,ArtifactEvidence(ref,'application/json',2,
            'encoded_manifest','output'),'photos','key-'+str(ref.artifact_id),'version-1') for ref in refs}
        calls=0
        inventory=refs
        def location(self,current,ref):
            assert current==scope
            self.calls+=1
            return self.values[ref]
        def outputs(self,current):
            assert current==scope
            return self.inventory
    class Objects:
        calls=[]
        def get_object(self,bucket,key,*,version_id):
            self.calls.append((bucket,key,version_id))
            return BytesIO(b'{}')
    authority=Authority();objects=Objects()
    return DeliveryMetadataEvidenceReader(authority,objects,scope=scope,allowed_bucket='photos'),authority,objects,refs


@pytest.mark.parametrize('mode',['version','key','scope','source','size','bucket'])
def test_reader_rechecks_authority_and_pins_location(mode):
    reader,authority,objects,refs=fixture();ref=refs[0]
    reader.evidence(ref)
    old=authority.values[ref]
    changes={'version':{'version':'version-2'},'key':{'key':'other'},
        'scope':{'scope':replace(old.scope,tenant_id=uuid4())},
        'source':{'evidence':replace(old.evidence,source='input')},
        'size':{'evidence':replace(old.evidence,bytes=3)},'bucket':{'bucket':'foreign'}}
    authority.values[ref]=replace(old,**changes[mode])
    with pytest.raises(AdoptionInvalid):reader.evidence(ref)
    assert authority.calls==2
    assert objects.calls==[]


def test_reader_pins_object_version_and_rechecks_after_io():
    reader,authority,objects,refs=fixture()
    assert reader.read_json(refs[0],100)==b'{}'
    assert authority.calls==3
    assert objects.calls==[('photos',authority.values[refs[0]].key,'version-1')]
    assert reader.staged_outputs()==set(refs)


@pytest.mark.parametrize('mode',['missing','extra','duplicate','input'])
def test_reader_requires_three_distinct_output_refs(mode):
    reader,authority,objects,refs=fixture()
    if mode=='missing':authority.inventory=refs[:2]
    if mode=='extra':authority.inventory=refs+(refs[0],)
    if mode=='duplicate':authority.inventory=(refs[0],refs[0],refs[1])
    if mode=='input':authority.values[refs[0]]=replace(authority.values[refs[0]],
        evidence=replace(authority.values[refs[0]].evidence,source='input'))
    with pytest.raises(AdoptionInvalid):reader.staged_outputs()


@pytest.mark.parametrize('version',['','null','bad\nversion','bad\x00version','x'*1025])
def test_reader_rejects_unsafe_versions(version):
    reader,authority,objects,refs=fixture()
    authority.values[refs[0]]=replace(authority.values[refs[0]],version=version)
    with pytest.raises(AdoptionInvalid):reader.read_json(refs[0],100)
    assert objects.calls==[]


def test_reader_cancellation_closes_object_response():
    reader,authority,objects,refs=fixture()
    response=BytesIO(b'{}')
    def get_object(*args,**kwargs):return response
    objects.get_object=get_object
    iterator=reader.iter_bytes(refs[0],100)
    assert next(iterator)==b'{}'
    def cancelled():raise RuntimeError('lease lost')
    reader.active=cancelled
    with pytest.raises(RuntimeError,match='lease lost'):next(iterator)
    assert response.closed
