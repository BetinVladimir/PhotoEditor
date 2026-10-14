from types import SimpleNamespace
from uuid import UUID, uuid4, uuid5
from hashlib import sha256
import json
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_import_sources import ImportSourceEntry, ImportSources, load_import_sources


def test_import_source_format_keeps_legacy_bytes_and_pins_sidecars():
    base = {'schema_version': 1, 'workflow_id': str(uuid4()), 'project_id': str(uuid4()),
        'parsed': {'artifact_id': str(uuid4()), 'sha256': 'a' * 64},
        'ingest_run_id': None,
        'entries': [{'ordinal': 0, 'entry_id': str(uuid4()), 'state': 'unsupported',
            'reason': 'unsupported_image_format', 'asset_version_id': None, 'master': None}]}
    legacy = ImportSources.model_validate_json(canonical_bytes(base))
    assert legacy.format == 'photoeditor_bundle'
    assert canonical_bytes(legacy) == canonical_bytes(base)
    for format in ('xmp', 'captureone_sidecar'):
        selected = ImportSources.model_validate_json(canonical_bytes({**base, 'format': format}))
        assert selected.format == format
        assert canonical_bytes(selected) == canonical_bytes({**base, 'format': format})
    with pytest.raises(ValueError):
        ImportSources.model_validate_json(canonical_bytes({**base, 'format': 'unknown'}))
    with pytest.raises(ValueError):
        ImportSources.model_validate_json(canonical_bytes({**base,
            'ingest_run_id': str(uuid5(UUID(base['workflow_id']), 'interchange:canonical-ingest:1'))}))
    for entries in ([{**base['entries'][0], 'ordinal': 1}],
                    [base['entries'][0], {**base['entries'][0], 'ordinal': 1}]):
        with pytest.raises(ValueError):
            ImportSources.model_validate_json(canonical_bytes({**base, 'entries': entries}))


@pytest.mark.parametrize('state', ['rejected','unsupported'])
def test_terminal_source_never_exposes_a_version_or_proposal_authority(state):
    value=dict(ordinal=0,entry_id=uuid4(),state=state,reason='unsupported_image_format',asset_version_id=None,master=None)
    assert ImportSourceEntry(**value).master is None
    value['asset_version_id']=uuid4()
    with pytest.raises(ValueError):
        ImportSourceEntry(**value)


def test_pending_ingest_returns_no_partial_inventory():
    raw=b'{}'
    calls=[]
    def execute(statement,args):
        calls.append(args)
        return SimpleNamespace(fetchone=lambda:(None,))
    tenant,workflow=uuid4(),uuid4()
    ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    assert load_import_sources(SimpleNamespace(execute=execute),tenant,workflow,ref,raw) is None
    assert calls==[(tenant,workflow,'{}')]


def test_substituted_parsed_bytes_never_query_sql():
    ref=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    db=SimpleNamespace(execute=lambda *args:pytest.fail('unexpected SQL'))
    with pytest.raises(AdoptionInvalid,match='SOURCE_INPUT'):
        load_import_sources(db,uuid4(),uuid4(),ref,b'{}')


def test_source_loader_rejects_mismatched_parsed_inventory():
    from test_interchange_ingest_snapshot import fixture
    raw, _ = fixture()
    parsed = json.loads(raw)
    parsed['files'][0]['contract'] = 'DifferentContract'
    raw = canonical_bytes(parsed)
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    workflow, project = uuid4(), uuid4()
    value = {'schema_version':1, 'workflow_id':str(workflow), 'project_id':str(project),
        'parsed':ref.model_dump(mode='json'), 'ingest_run_id':None,
        'entries':[{'ordinal':ordinal, 'entry_id':entry['entry_id'], 'state':'unsupported',
            'reason':'unsupported_image_format', 'asset_version_id':None, 'master':None}
            for ordinal, entry in enumerate(parsed['manifest']['entries'])]}
    db = SimpleNamespace(execute=lambda *args:SimpleNamespace(fetchone=lambda:(value,)))
    with pytest.raises(AdoptionInvalid, match='SOURCE_INVENTORY'):
        load_import_sources(db, uuid4(), workflow, ref, raw)


def test_source_loader_refuses_spurious_child_run_even_with_canonical_id():
    from test_interchange_ingest_snapshot import fixture
    raw, _ = fixture()
    parsed = json.loads(raw)
    ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    workflow, project = uuid4(), uuid4()
    value = {'schema_version': 1, 'workflow_id': str(workflow), 'project_id': str(project),
        'parsed': ref.model_dump(mode='json'),
        'ingest_run_id': str(uuid5(workflow, 'interchange:canonical-ingest:1')),
        'entries': [{'ordinal': ordinal, 'entry_id': entry['entry_id'], 'state': 'unsupported',
            'reason': 'unsupported_image_format', 'asset_version_id': None, 'master': None}
            for ordinal, entry in enumerate(parsed['manifest']['entries'])]}
    db = SimpleNamespace(execute=lambda *args: SimpleNamespace(fetchone=lambda: (value,)))
    with pytest.raises(AdoptionInvalid, match='SOURCE_MISMATCH'):
        load_import_sources(db, uuid4(), workflow, ref, raw)
    value['ingest_run_id'] = str(uuid4())
    with pytest.raises(AdoptionInvalid, match='SOURCE_MISMATCH'):
        load_import_sources(db, uuid4(), workflow, ref, raw)
