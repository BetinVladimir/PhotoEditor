from uuid import uuid4
from hashlib import sha256
import json
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_archive import ArchiveLimits
from photo_worker.v2.interchange_plan_compile import compile_import_plan
from test_interchange_import_stage import adopted_fixture


def compile(body,sources,read,**kwargs):
    return compile_import_plan(parsed_jcs=body,sources=sources,snapshot_digest='f'*64,
        limits=ArchiveLimits(1<<30,2<<30,30000),prepared_recipes=kwargs.pop('prepared_recipes',{}),
        read_document=read,**kwargs)


def test_mixed_plan_preserves_every_entry_and_binds_only_ingested_source():
    body,sources,_,image=adopted_fixture();calls=[]
    def read(ref,limit):
        calls.append((ref,limit));return image
    plan=compile(body,sources,read)
    assert len(plan.entries)==4 and len(calls)==1
    assert plan.entries[0].binding.asset_version_id==sources.entries[0].asset_version_id
    assert all(e.binding is None and e.recipe is None for e in plan.entries[1:])
    assert plan.entries[0].recipe is None
    sources.entries[0].asset_version_id=uuid4()
    assert plan.entries[0].binding.asset_version_id!=sources.entries[0].asset_version_id


def test_plan_format_follows_frozen_source_format_without_reusing_bundle_hash():
    from photo_worker.v2.interchange_import_sources import ImportSources
    body, sources, _, image = adopted_fixture()
    bundle = compile(body, sources, lambda *_: image)
    assert bundle.format == 'photoeditor_bundle'
    for format in ('xmp', 'captureone_sidecar'):
        selected = ImportSources.model_validate_json(canonical_bytes({
            **sources.model_dump(mode='json'), 'format': format}))
        plan = compile(body, selected, lambda *_: image)
        assert plan.format == format
        assert sha256(canonical_bytes(plan)).hexdigest() != sha256(canonical_bytes(bundle)).hexdigest()


def test_all_terminal_plan_never_reads_master_or_recipe():
    body,sources,_,_=adopted_fixture()
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.v2.interchange_import_sources import ImportSources
    wire=sources.model_dump(mode='json')
    wire['entries'][0].update(state='unsupported',reason='unsupported_image_format',asset_version_id=None,master=None)
    wire['ingest_run_id'] = None
    sources=ImportSources.model_validate_json(canonical_bytes(wire))
    def read(*args):pytest.fail('terminal source object IO')
    plan=compile(body,sources,read)
    assert len(plan.entries)==4 and all(e.binding is None for e in plan.entries)


def test_plan_rejects_substituted_master_bytes():
    body,sources,_,_=adopted_fixture()
    with pytest.raises(AdoptionInvalid,match='MASTER_HASH'):compile(body,sources,lambda *a:b'{}')


def test_plan_rejects_foreign_proposal_before_object_io():
    body,sources,_,_=adopted_fixture()
    with pytest.raises(AdoptionInvalid,match='FOREIGN_PROPOSAL'):
        compile(body,sources,lambda *a:pytest.fail('unexpected IO'),prepared_recipes={uuid4():object()})


def test_plan_rejects_source_reordering_before_object_io():
    body,sources,_,_=adopted_fixture();sources.entries.reverse()
    with pytest.raises(AdoptionInvalid,match='SOURCE_IDENTITY'):
        compile(body,sources,lambda *a:pytest.fail('unexpected IO'))


def test_plan_rejects_parsed_file_contract_changed_before_object_io():
    body, sources, _, _ = adopted_fixture()
    parsed = json.loads(body)
    parsed['files'][0]['contract'] = 'OtherContract'
    body = canonical_bytes(parsed)
    sources.parsed.sha256 = sha256(body).hexdigest()
    with pytest.raises(AdoptionInvalid, match='PLAN_INVENTORY'):
        compile(body, sources, lambda *a:pytest.fail('unexpected IO'))
