import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_binding_qualification import CanonicalImportBindings
from photo_worker.v2.interchange_parse import ParsedInterchange
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.interchange_remap import UnsupportedDependency
from test_interchange_import_stage import adopted_fixture
from test_interchange_recipe import binding


def test_binding_without_recipe_does_not_invent_original_identity():
    body,sources,_,_=adopted_fixture()
    parsed=ParsedInterchange.model_validate_json(body)
    def no_io(*args):pytest.fail('recipe absent: no identity IO expected')
    qualifier=CanonicalImportBindings(parsed,sources,no_io)
    with pytest.raises(UnsupportedDependency,match='UNQUALIFIED'):
        qualifier(binding())


def test_binding_changed_source_scope_fails_before_io():
    body,sources,_,_=adopted_fixture()
    parsed=ParsedInterchange.model_validate_json(body)
    sources.parsed.sha256='f'*64
    with pytest.raises(AdoptionInvalid,match='SOURCE_SCOPE'):
        CanonicalImportBindings(parsed,sources,lambda *args:pytest.fail('unexpected IO'))


def test_binding_cancelled_before_reconstruction():
    body,sources,_,_=adopted_fixture()
    def cancelled():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        CanonicalImportBindings(ParsedInterchange.model_validate_json(body),sources,lambda *args:b'',check_active=cancelled)


def test_verified_recipe_and_original_image_rebind_to_ingested_version():
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef,canonical_hash
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.operations.session.contracts import VersionBinding
    from test_interchange_recipe import fixture as recipe_fixture
    body,sources,_,image_raw=adopted_fixture()
    parsed=ParsedInterchange.model_validate_json(body)
    image=WorkingImage.model_validate_json(image_raw)
    old_image_id=uuid4()
    old=VersionBinding(asset_version_id=uuid4(),source=ArtifactRef(artifact_id=old_image_id,sha256=sha256(image_raw).hexdigest()),canvas_hash=canonical_hash(image.canvas))
    recipe,_,_,_,_=recipe_fixture()
    recipe.source=old
    recipe_raw=canonical_bytes(recipe)
    value=parsed.model_dump(mode='json')
    files={sources.entries[0].master.artifact_id:image_raw}
    for original,path,raw,contract in [(old_image_id,'original-working.json',image_raw,'WorkingImage'),(uuid4(),'authored-recipe.json',recipe_raw,'EditingRecipeV3')]:
        digest=sha256(raw).hexdigest();gateway_id=uuid4();files[gateway_id]=raw
        value['manifest']['inventory'].append({'artifact_id':str(original),'path':path,'sha256':digest,'contract':contract})
        value['files'].append({'path':path,'content':{'artifact_id':str(gateway_id),'sha256':digest},'contract':contract})
    value['manifest']['entries'][0]['recipe_path']='authored-recipe.json'
    value['manifest']['entries'][0]['recipe_sha256']=sha256(recipe_raw).hexdigest()
    parsed=ParsedInterchange.model_validate_json(canonical_bytes(value))
    sources.parsed.sha256=canonical_hash(parsed)
    calls=[]
    def read(ref,maximum):
        calls.append(ref.artifact_id)
        return files[ref.artifact_id]
    qualifier=CanonicalImportBindings(parsed,sources,read)
    target=qualifier(old)
    assert target.asset_version_id==sources.entries[0].asset_version_id
    assert target.source==sources.entries[0].master
    assert target.canvas_hash==canonical_hash(image.canvas)
    assert len(calls)==3
    target.canvas_hash='f'*64
    assert qualifier(old).canvas_hash==canonical_hash(image.canvas)
    assert len(calls)==3


def malformed_recipe_fixture():
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import canonical_hash
    body,sources,_,_=adopted_fixture()
    value=ParsedInterchange.model_validate_json(body).model_dump(mode='json')
    raw=b'{"schema_version":99}'
    digest=sha256(raw).hexdigest()
    value['manifest']['entries'][0]['recipe_path']='malformed.json'
    value['manifest']['entries'][0]['recipe_sha256']=digest
    value['manifest']['inventory'].append({'artifact_id':str(uuid4()),'path':'malformed.json','sha256':digest,'contract':'EditingRecipeV3'})
    value['files'].append({'path':'malformed.json','content':{'artifact_id':str(uuid4()),'sha256':digest},'contract':'EditingRecipeV3'})
    parsed=ParsedInterchange.model_validate_json(canonical_bytes(value))
    sources.parsed.sha256=canonical_hash(parsed)
    return parsed,sources,raw


def test_unsupported_recipe_is_recorded_without_aborting_inventory():
    parsed,sources,raw=malformed_recipe_fixture()
    qualifier=CanonicalImportBindings(parsed,sources,lambda *args:raw)
    assert qualifier.entry_failures=={parsed.manifest.entries[0].entry_id:'INTERCHANGE_BINDING_TYPED_DOCUMENT_INVALID'}
    assert qualifier.bindings=={}


def test_storage_error_and_hash_violation_are_never_reported_as_unsupported():
    parsed,sources,raw=malformed_recipe_fixture()
    def offline(*args):raise OSError('object unavailable')
    with pytest.raises(OSError):
        CanonicalImportBindings(parsed,sources,offline)
    with pytest.raises(AdoptionInvalid,match='DOCUMENT_HASH'):
        CanonicalImportBindings(parsed,sources,lambda *args:b'{"changed":true}')
