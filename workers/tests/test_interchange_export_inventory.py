from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.interchange_export_inventory import compile_qualified_export_inventory
from photo_worker.v2.interchange_recipe_closure import PreparedRecipeClosureQualifier
from photo_worker.v2.interchange_read_budget import ImportReadBudget
from test_interchange_export_prepare import fixture
from test_interchange_recipe_closure import closure_fixture


def inventory_fixture():
    recipe, root, args = closure_fixture()
    plan, _, _ = fixture();plan.entries[0].recipe=root;plan.entries[0].binding=recipe.source
    entries = {(ref.artifact_id, ref.sha256):SimpleNamespace(contract=contract, ref=ref)
               for ref, contract, _ in args['entries']}
    image=plan.entries[0].content
    entries[(image.artifact_id,image.sha256)]=SimpleNamespace(contract='EncodedImage',ref=image)
    # Unrelated declared artifact must never become an exported file.
    unrelated=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    entries[(unrelated.artifact_id,unrelated.sha256)]=SimpleNamespace(contract='StyleV2',ref=unrelated)
    class Reader:
        read_budget=ImportReadBudget(128 << 20)
        rechecks=0
        def __init__(self):self.entries=entries
        def evidence(self,reference):return SimpleNamespace(bytes=5 if reference==image else len(args['read_document'](reference,8 << 20)) if reference!=unrelated else 2,media_type='image/tiff' if reference==image else 'application/json')
        def read_json(self,reference,maximum):
            raw=args['read_document'](reference,maximum)
            assert len(raw)<=maximum;self.read_budget.consume(len(raw),is_json=True);return raw
        def recheck(self):self.rechecks+=1
    return plan,Reader(),PreparedRecipeClosureQualifier(**args),unrelated


def test_export_inventory_uses_only_successful_recipe_closure():
    plan,reader,qualifier,unrelated=inventory_fixture()
    prepared=compile_qualified_export_inventory(plan,reader,qualifier)
    ids={source.reference.artifact_id for source in prepared.sources}
    assert unrelated.artifact_id not in ids
    assert plan.entries[0].content.artifact_id in ids and plan.entries[0].recipe.artifact_id in ids
    assert len(prepared.sources)==8 and reader.rechecks==1
    refs=prepared.recipe_closure(plan.entries[0])
    assert len(refs)==7
    refs[0].sha256='f'*64
    assert prepared.recipe_closure(plan.entries[0])[0]==plan.entries[0].recipe
    assert [item.path for item in prepared.sources]==sorted(item.path for item in prepared.sources)


@pytest.mark.parametrize('mode',['qualifier','contract','files','recipe_hash'])
def test_export_inventory_rejects_unqualified_or_unbound_sources(mode):
    plan,reader,qualifier,_=inventory_fixture()
    if mode=='qualifier':
        class Denied:
            def __call__(self,*args):return False
            def qualified_document_refs(self,*args):pytest.fail('denied qualification requested receipts')
        qualifier=Denied()
    elif mode=='contract':reader.entries[(plan.entries[0].content.artifact_id,plan.entries[0].content.sha256)].contract='Artifact'
    elif mode=='files':plan.max_files=3
    elif mode=='recipe_hash':
        original=reader.read_json
        reader.read_json=lambda reference,maximum:original(reference,maximum)+b' '
    with pytest.raises(ValueError):compile_qualified_export_inventory(plan,reader,qualifier)
