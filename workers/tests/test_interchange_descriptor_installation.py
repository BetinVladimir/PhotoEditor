from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash, parse_json
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_descriptor_installation import load_export_descriptor_pins
from test_interchange_descriptor_occurrences import fixture


def setup():
    pin, _ = fixture()
    snapshot_id = uuid4()
    items = [dict(ordinal=0, binding=pin['selected_binding'], working_image=pin['selected_binding'].source,
        captured_at=None, camera_hash=None, recipe=pin['root'])]
    digest = canonical_hash(dict(schema_version=1, snapshot_id=snapshot_id, items=items))
    raw = canonical_bytes(dict(schema_version=1, snapshot_id=snapshot_id, snapshot_digest=digest, items=items))
    snapshot = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    recipe = ArtifactRef(artifact_id=uuid4(), sha256=parse_json(pin['root_body'])['recipe_sha256'])
    plan = dict(schema_version=1, project_id=uuid4(), direction='export', format='photoeditor_bundle',
        snapshot_digest=digest, entries=[dict(entry_id=uuid4(), external_id='one', path='one.tif',
            content=ArtifactRef(artifact_id=uuid4(), sha256='a' * 64), binding=pin['selected_binding'],
            recipe=recipe, rating=0, pick='unmarked', crop=None)], max_archive_bytes=1 << 20,
        max_expanded_bytes=1 << 20, max_files=100)
    bodies = {snapshot.artifact_id: raw, pin['root'].artifact_id: pin['root_body'],
        pin['descriptor'].artifact_id: pin['descriptor_body'],
        recipe.artifact_id: canonical_bytes(parse_json(pin['root_body'])['recipe'])}
    entries = {(ref.artifact_id, ref.sha256): SimpleNamespace(contract=contract) for ref, contract in
        [(snapshot,'InputSnapshot'), (pin['root'],'FrozenRenderRecipe'), (pin['descriptor'],'DescriptorShard'), (recipe,'EditingRecipeV3')]}
    reader = SimpleNamespace(entries=entries, rechecks=0)
    reader.read_json = lambda ref, maximum: bodies[ref.artifact_id]
    def recheck(): reader.rechecks += 1
    reader.recheck = recheck
    return pin, snapshot, plan, reader, bodies


def test_descriptor_pin_loader_derives_exact_frozen_and_analysis_context():
    pin, snapshot, plan, reader, _ = setup()
    pins = load_export_descriptor_pins(reader, snapshot, plan)
    assert len(pins) == 1 and pins[0] == pin
    assert reader.rechecks == 1


@pytest.mark.parametrize('mode', ['digest','binding','recipe','undeclared','contract','bytes','revoked'])
def test_descriptor_pin_loader_rejects_changed_declared_inputs(mode):
    pin, snapshot, plan, reader, bodies = setup()
    if mode == 'digest': plan['snapshot_digest'] = 'f' * 64
    elif mode == 'binding': plan['entries'][0]['binding'] = plan['entries'][0]['binding'].model_copy(update={'canvas_hash':'f' * 64})
    elif mode == 'recipe': plan['entries'][0]['recipe'].sha256 = 'f' * 64
    elif mode == 'undeclared': del reader.entries[(pin['descriptor'].artifact_id,pin['descriptor'].sha256)]
    elif mode == 'contract': reader.entries[(pin['descriptor'].artifact_id,pin['descriptor'].sha256)].contract = 'CandidateProfile'
    elif mode == 'bytes': bodies[pin['root'].artifact_id] += b' '
    elif mode == 'revoked':
        def recheck(): raise AdoptionInvalid('revoked inputs')
        reader.recheck = recheck
    with pytest.raises((AdoptionInvalid, ValueError)):
        load_export_descriptor_pins(reader, snapshot, plan)


@pytest.mark.parametrize('mode', ['valid', 'missing', 'extra', 'wrong_hash', 'stale_params', 'revoked'])
def test_sql_descriptor_factory_uses_admitted_snapshot_and_task_context(mode):
    from photo_worker.v2.interchange_descriptor_installation import SQLExportDescriptorOccurrenceFactory
    from photo_worker.v2.interchange_plan import InterchangePlan, InterchangeExportParams
    pin, snapshot, plan_wire, reader, _ = setup()
    plan = InterchangePlan.model_validate_json(canonical_bytes(plan_wire))
    params = InterchangeExportParams(schema_version=1,
        plan=ArtifactRef(artifact_id=uuid4(), sha256='a' * 64), max_report_bytes=8388608)
    raw = canonical_bytes(params)
    command = SimpleNamespace(operation='v2.interchange.export', operation_version='1', resource_class='cpu',
        params=ArtifactRef(artifact_id=uuid4(), sha256='f' * 64 if mode=='stale_params' else sha256(raw).hexdigest()))
    ctx = SimpleNamespace(lease=SimpleNamespace(command=command), check_active=lambda: None)
    calls = []
    def lookup(lease, actual_params):
        assert lease is ctx.lease and actual_params == raw
        calls.append('snapshot')
        if mode == 'missing': return None
        value = snapshot.model_dump(mode='json')
        if mode == 'extra': value['approved'] = True
        if mode == 'wrong_hash': value['sha256'] = 'f' * 64
        return value
    def current(lease, actual_params, evidence):
        assert lease is ctx.lease and actual_params == raw
        calls.append(evidence)
        return mode != 'revoked'
    database = SimpleNamespace(interchange_export_snapshot=lookup,
        interchange_export_descriptor_authority=current)
    authority = SimpleNamespace(project_id=plan.project_id, snapshot_digest=plan.snapshot_digest)
    factory = SQLExportDescriptorOccurrenceFactory(database, qualify_other=lambda *args: False)
    artifacts = SimpleNamespace(params=lambda model: params)
    if mode in ('missing', 'extra', 'wrong_hash', 'stale_params'):
        with pytest.raises(ValueError): factory(ctx, artifacts, authority, plan, reader)
        assert calls == ([] if mode=='stale_params' else ['snapshot'])
        return
    policy = factory(ctx, artifacts, authority, plan, reader)
    event = dict(reference=pin['descriptor'], contract='DescriptorShard', parent=pin['root'],
        parent_contract='FrozenRenderRecipe', path=('learning','descriptor'),
        parent_body=parse_json(pin['root_body']), binding=pin['selected_binding'])
    if mode == 'revoked':
        with pytest.raises(AdoptionInvalid, match='AUTHORITY'): policy(event)
    else:
        assert policy(event) is True
        assert calls[1]['root_jcs'].encode() == pin['root_body']
        assert calls[1]['frame_binding'] is None


@pytest.mark.parametrize('missing', ['snapshot', 'current', 'other'])
def test_sql_descriptor_factory_refuses_incomplete_installation(missing):
    from photo_worker.v2.interchange_descriptor_installation import SQLExportDescriptorOccurrenceFactory
    database = SimpleNamespace(interchange_export_snapshot=lambda *args: None,
        interchange_export_descriptor_authority=lambda *args: True)
    if missing == 'snapshot': database.interchange_export_snapshot = None
    if missing == 'current': database.interchange_export_descriptor_authority = None
    with pytest.raises(ValueError, match='INSTALLATION'):
        SQLExportDescriptorOccurrenceFactory(database,
            qualify_other=None if missing == 'other' else lambda *args: True)


@pytest.mark.parametrize('ordinal', [1, 7, 4999])
def test_descriptor_loader_rejects_noncontiguous_snapshot_ordinals(ordinal):
    _, snapshot, plan, reader, bodies = setup()
    value = parse_json(bodies[snapshot.artifact_id])
    value['items'][0]['ordinal'] = ordinal
    value['snapshot_digest'] = canonical_hash({key: value[key] for key in ('schema_version','snapshot_id','items')})
    raw = canonical_bytes(value)
    replacement = ArtifactRef(artifact_id=snapshot.artifact_id, sha256=sha256(raw).hexdigest())
    bodies[snapshot.artifact_id] = raw
    reader.entries[(replacement.artifact_id,replacement.sha256)] = SimpleNamespace(contract='InputSnapshot')
    plan['snapshot_digest'] = value['snapshot_digest']
    with pytest.raises(AdoptionInvalid, match='SNAPSHOT'):
        load_export_descriptor_pins(reader, replacement, plan)


@pytest.mark.parametrize('mode', ['absent', 'contract', 'bytes', 'alias'])
def test_descriptor_loader_requires_exact_declared_portable_recipe(mode):
    pin, snapshot, plan, reader, bodies = setup()
    recipe = plan['entries'][0]['recipe']
    if mode == 'absent': del reader.entries[(recipe.artifact_id,recipe.sha256)]
    elif mode == 'contract': reader.entries[(recipe.artifact_id,recipe.sha256)].contract = 'FrozenRenderRecipe'
    elif mode == 'bytes': bodies[recipe.artifact_id] += b' '
    elif mode == 'alias':
        plan['entries'][0]['recipe'] = ArtifactRef(artifact_id=pin['root'].artifact_id, sha256=recipe.sha256)
    with pytest.raises(AdoptionInvalid):
        load_export_descriptor_pins(reader, snapshot, plan)


def test_snapshot_ordinals_do_not_replace_original_descriptor_ordinals():
    pin, snapshot, plan, reader, _ = setup()
    assert pin['selected_ordinal'] == 7
    pins = load_export_descriptor_pins(reader, snapshot, plan)
    assert pins[0]['selected_ordinal'] == 7


@pytest.mark.parametrize('field', ['binding', 'recipe'])
def test_descriptor_loader_refuses_incomplete_export_selection(field):
    _, snapshot, plan, reader, _ = setup()
    plan['entries'][0][field] = None
    if field == 'binding': plan['entries'][0]['recipe'] = None
    with pytest.raises(AdoptionInvalid, match='SELECTION'):
        load_export_descriptor_pins(reader, snapshot, plan)


@pytest.mark.parametrize('ordinals', [[1, 0], [0, 0], [0, 2]])
def test_descriptor_loader_rejects_reordered_duplicate_or_gapped_snapshots(ordinals):
    from copy import deepcopy
    _, snapshot, plan, reader, bodies = setup()
    value = parse_json(bodies[snapshot.artifact_id])
    second = deepcopy(value['items'][0])
    second['binding']['asset_version_id'] = str(uuid4())
    value['items'].append(second)
    for item, ordinal in zip(value['items'], ordinals): item['ordinal'] = ordinal
    value['snapshot_digest'] = canonical_hash({key: value[key] for key in ('schema_version','snapshot_id','items')})
    raw = canonical_bytes(value)
    replacement = ArtifactRef(artifact_id=snapshot.artifact_id, sha256=sha256(raw).hexdigest())
    bodies[snapshot.artifact_id] = raw
    reader.entries[(replacement.artifact_id,replacement.sha256)] = SimpleNamespace(contract='InputSnapshot')
    plan['snapshot_digest'] = value['snapshot_digest']
    with pytest.raises(AdoptionInvalid, match='SNAPSHOT'):
        load_export_descriptor_pins(reader, replacement, plan)
