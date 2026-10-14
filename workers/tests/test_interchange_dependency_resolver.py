import json
from hashlib import sha256
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.v2.interchange_parse import ParsedInterchange
from photo_worker.v2.interchange_dependency_resolver import BundleRecipeDependencyResolver
from photo_worker.v2.interchange_remap import UnsupportedDependency
from test_interchange_remap import fixture
from test_interchange_recipe import binding


def correction_fixture():
    parsed,identifier,_,_,scope=fixture()
    old,target=binding(),binding()
    raw=canonical_bytes({'schema_version':1,'snapshot_digest':'c'*64,'items':[{
        'binding':old.model_dump(mode='json'),'ordinal':0,'delta':{'ev':0.25,'log_r':0.0,'log_g':0.0,'log_b':0.0,
        'saturation_delta':0.05,'retouch_delta':0.0},'disposition':'apply','reason':None}]})
    value=parsed.model_dump(mode='json')
    for item in value['manifest']['inventory']:
        if item['path']=='recipe.json':item.update(contract='CorrectionPlan',sha256=sha256(raw).hexdigest())
    value['files'][1]['contract']='CorrectionPlan';value['files'][1]['content']['sha256']=sha256(raw).hexdigest()
    value['manifest']['entries'][0]['recipe_sha256']=sha256(raw).hexdigest()
    return ParsedInterchange.model_validate_json(canonical_bytes(value)),identifier,scope,old,target,raw


def test_typed_correction_dependency_rebinds_without_changing_adjustments():
    parsed,identifier,scope,old,target,raw=correction_fixture()
    publications=[]
    def publish(doc):
        publications.append(doc)
        return ArtifactRef(artifact_id=doc.artifact_id,sha256=doc.sha256)
    resolver=BundleRecipeDependencyResolver(parsed=parsed,tenant=scope[0],project=scope[1],job=scope[2],
        read_document=lambda *args:raw,publish_document=publish,qualify_binding=lambda value:target,
        qualify_binary=lambda *args:pytest.fail('unexpected binary'),qualify_document=lambda *args:None)
    original=ArtifactRef(artifact_id=identifier,sha256=sha256(raw).hexdigest())
    resolved=resolver(original,'CorrectionPlan',target)
    assert resolved.binding==target and resolved.replacement.artifact_id!=identifier
    result=json.loads(publications[0].body)
    assert result['items'][0]['binding']==target.model_dump(mode='json')
    assert result['items'][0]['delta']['ev']==0.25
    assert result['items'][0]['delta']['saturation_delta']==0.05
    assert resolver(original,'CorrectionPlan',target)==resolved
    assert len(publications)==1
    with pytest.raises(UnsupportedDependency,match='CANONICAL_BINDING'):
        resolver(original,'CorrectionPlan',binding())


def test_type_mismatch_never_reads_or_publishes():
    parsed,identifier,scope,old,target,raw=correction_fixture()
    def no_io(*args):pytest.fail('unexpected IO')
    resolver=BundleRecipeDependencyResolver(parsed=parsed,tenant=scope[0],project=scope[1],job=scope[2],
        read_document=no_io,publish_document=no_io,qualify_binding=no_io,qualify_binary=no_io,qualify_document=no_io)
    with pytest.raises(UnsupportedDependency,match='TYPE_OR_HASH'):
        resolver(ArtifactRef(artifact_id=identifier,sha256=sha256(raw).hexdigest()),'LayerStack',target)
