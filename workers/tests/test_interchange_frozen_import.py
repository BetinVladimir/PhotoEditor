import json
from hashlib import sha256
from uuid import uuid4

import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_frozen_import import prepare_frozen_import_stage
from photo_worker.v2.interchange_import_sources import ImportSources
from test_interchange_import_stage import adopted_fixture
from test_interchange_plan_compile import compile


def arguments():
    body, sources, receipts, image = adopted_fixture()
    plan = compile(body, sources, lambda *args: image)
    raw = canonical_bytes(plan)
    publications = []
    def publish(raw, *args):
        publications.append(raw)
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    return dict(plan_ref=ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest()),
                plan_jcs=raw, parsed_jcs=body, sources=sources, snapshot_digest='f'*64,
                read_document=lambda *args: image, publish_document=publish,
                metadata_receipts=receipts), publications


def test_frozen_import_reports_every_entry_without_recompiling_recipes():
    args, publications = arguments()
    result = prepare_frozen_import_stage(**args)
    report = json.loads(result.report_jcs)
    assert report['entry_count'] == 4
    assert result.proposals == () and len(publications) == 2
    entries = json.loads(result.shard_bodies[0])['entries']
    assert len(entries) == 4
    assert {field['name']: field['status'] for field in entries[0]['fields']} == {
        'image': 'exact', 'rating': 'exact', 'pick': 'exact', 'crop': 'exact', 'recipe': 'unsupported'}


@pytest.mark.parametrize('change', ['hash', 'metadata', 'binding', 'order', 'format', 'diagnostics', 'receipt'])
def test_frozen_import_refuses_inconsistent_plan_before_read_or_publication(change):
    args, publications = arguments()
    plan = json.loads(args['plan_jcs'])
    if change == 'hash':
        args['plan_ref'].sha256 = '0'*64
    elif change == 'metadata':
        plan['entries'][0]['rating'] = 5
    elif change == 'binding':
        plan['entries'][0]['binding']['asset_version_id'] = str(uuid4())
    elif change == 'order':
        plan['entries'].reverse()
    elif change == 'format':
        plan['format'] = 'xmp'
    elif change == 'diagnostics':
        args['recipe_diagnostics'] = {uuid4(): 'unrelated'}
    else:
        args['metadata_receipts'] = None
    if change in ('metadata', 'binding', 'order', 'format'):
        args['plan_jcs'] = canonical_bytes(plan)
        args['plan_ref'].sha256 = sha256(args['plan_jcs']).hexdigest()
    args['read_document'] = lambda *args: pytest.fail('invalid preflight reached object IO')
    with pytest.raises((AdoptionInvalid, ValueError)):
        prepare_frozen_import_stage(**args)
    assert publications == []


def test_frozen_import_all_terminal_preserves_reasons_without_object_io():
    args, publications = arguments()
    wire = args['sources'].model_dump(mode='json')
    wire['entries'][0].update(state='rejected', reason='asset.decode:failed', asset_version_id=None, master=None)
    args['sources'] = ImportSources.model_validate_json(canonical_bytes(wire))
    plan = compile(args['parsed_jcs'], args['sources'], lambda *args: pytest.fail('terminal compiler IO'))
    args['plan_jcs'] = canonical_bytes(plan)
    args['plan_ref'].sha256 = sha256(args['plan_jcs']).hexdigest()
    args['metadata_receipts'] = None
    args['read_document'] = lambda *args: pytest.fail('terminal stage IO')
    result = prepare_frozen_import_stage(**args)
    entries = json.loads(result.shard_bodies[0])['entries']
    assert entries[0]['fields'] == [{'name': 'image', 'reason': 'asset.decode:failed', 'status': 'rejected'}]
    assert result.proposals == () and len(publications) == 2


def test_frozen_import_cancellation_prevents_publication():
    args, publications = arguments()
    class Cancelled(Exception):
        pass
    def read(*unused):
        args['check_active'] = lambda: None
        raise Cancelled()
    args['read_document'] = read
    with pytest.raises(Cancelled):
        prepare_frozen_import_stage(**args)
    assert publications == []


@pytest.mark.parametrize('qualified', [True, False, 1])
def test_frozen_import_uses_prepared_recipe_only_after_full_closure_qualification(qualified):
    from test_interchange_recipe import fixture as recipe_fixture
    from photo_worker.v2.interchange_plan import InterchangePlan
    args, publications = arguments()
    plan = InterchangePlan.model_validate_json(args['plan_jcs'])
    recipe, _, _, _, _ = recipe_fixture()
    recipe.source = plan.entries[0].binding.model_copy(deep=True)
    recipe_raw = canonical_bytes(recipe)
    prepared_ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(recipe_raw).hexdigest())
    plan.entries[0].recipe = prepared_ref
    parsed = json.loads(args['parsed_jcs'])
    path = 'sidecars/prepared-original.json'
    parsed['manifest']['entries'][0].update(recipe_path=path, recipe_sha256=prepared_ref.sha256)
    parsed['manifest']['inventory'].append(dict(artifact_id=str(uuid4()), path=path,
        sha256=prepared_ref.sha256, contract='EditingRecipeV3'))
    original_ref = ArtifactRef(artifact_id=uuid4(), sha256=prepared_ref.sha256)
    parsed['files'].append(dict(path=path, content=original_ref.model_dump(mode='json'), contract='EditingRecipeV3'))
    args['parsed_jcs'] = canonical_bytes(parsed)
    args['sources'].parsed.sha256 = sha256(args['parsed_jcs']).hexdigest()
    args['metadata_receipts'].parsed.sha256 = args['sources'].parsed.sha256
    args['plan_jcs'] = canonical_bytes(plan)
    args['plan_ref'].sha256 = sha256(args['plan_jcs']).hexdigest()
    old_read = args['read_document']
    reads, qualifications = [], []
    def read(ref, limit):
        reads.append(ref)
        return recipe_raw if ref == prepared_ref else old_read(ref, limit)
    def qualify(ref, document, binding):
        qualifications.append(ref)
        assert ref == prepared_ref and document.source == binding
        return qualified
    args.update(read_document=read, qualify_recipe=qualify)
    if qualified is not True:
        with pytest.raises(AdoptionInvalid, match='RECIPE_CLOSURE'):
            prepare_frozen_import_stage(**args)
        assert publications == []
    else:
        result = prepare_frozen_import_stage(**args)
        assert result.proposals == (prepared_ref,)
        assert original_ref not in reads
    assert qualifications == [prepared_ref]


@pytest.mark.parametrize('corrupt', [False, True])
def test_runtime_frozen_import_reads_pinned_xmp_and_reports_unknown_properties(corrupt):
    from pathlib import PurePosixPath
    from photo_worker.v2.interchange_xmp import XMPMapping, export_xmp, CRS
    args, publications = arguments()
    parsed = json.loads(args['parsed_jcs'])
    path = str(PurePosixPath(parsed['manifest']['entries'][0]['image_path']).with_suffix('.xmp'))
    xmp = export_xmp(XMPMapping(0, 'unmarked', 1, None, ()))
    xmp = xmp.replace(b'rdf:about=""', b'rdf:about="" crs:Exposure2012="2"')
    digest = sha256(xmp).hexdigest()
    ref = ArtifactRef(artifact_id=uuid4(), sha256=digest)
    parsed['manifest']['inventory'].append({'artifact_id':str(uuid4()),'path':path,'sha256':digest,'contract':'XMP'})
    parsed['files'].append({'path':path,'content':ref.model_dump(mode='json'),'contract':'XMP'})
    args['parsed_jcs'] = canonical_bytes(parsed)
    args['sources'].parsed.sha256 = sha256(args['parsed_jcs']).hexdigest()
    args['metadata_receipts'].parsed.sha256 = args['sources'].parsed.sha256
    original_read = args['read_document']
    reads = []

    def read(candidate, limit):
        reads.append(candidate.model_copy(deep=True))
        if candidate == ref:
            return b'corrupt XMP' if corrupt else xmp
        return original_read(candidate, limit)

    args['read_document'] = read
    if corrupt:
        with pytest.raises(AdoptionInvalid):
            prepare_frozen_import_stage(**args)
        assert publications == []
    else:
        result = prepare_frozen_import_stage(**args)
        first = json.loads(result.shard_bodies[0])['entries'][0]
        field = next(field for field in first['fields'] if field['name'] == 'xmp_properties')
        expected = sha256(canonical_bytes([CRS + '}Exposure2012'])).hexdigest()
        assert field['reason'] == 'unsupported_xmp_properties:1:' + expected
        assert field['status'] == 'unsupported'
        assert json.loads(result.report_jcs)['unsupported_count'] == 4
    assert reads == [args['sources'].entries[0].master, ref]
