from hashlib import sha256
from uuid import uuid4, uuid5
import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_import_evidence import freeze_import_evidence, read_import_evidence
from test_interchange_import_stage import adopted_fixture
from test_interchange_plan_compile import compile


def evidence_arguments():
    parsed, sources, metadata, image = adopted_fixture()
    plan = compile(parsed, sources, lambda *args: image)
    task = uuid5(sources.workflow_id, 'v2.interchange.import:0')
    ref = ArtifactRef(artifact_id=uuid5(task, 'interchange:plan:1'), sha256=sha256(canonical_bytes(plan)).hexdigest())
    return dict(workflow_id=sources.workflow_id, plan_ref=ref, plan=plan,
                parsed_jcs=parsed, sources=sources, metadata=metadata, recipe_diagnostics={})


def test_import_evidence_preserves_terminal_states_and_is_deterministic():
    args = evidence_arguments();prepared = freeze_import_evidence(**args)
    assert freeze_import_evidence(**args) == prepared
    document = read_import_evidence(prepared.reference, prepared.body, **{
        key: args[key] for key in ('workflow_id', 'plan_ref', 'plan', 'parsed_jcs')})
    assert document.sources == args['sources'] and document.metadata == args['metadata']
    args['sources'].entries[1].reason = 'caller-mutated'
    assert b'caller-mutated' not in prepared.body


@pytest.mark.parametrize('change', ['workflow', 'plan_ref', 'plan_hash', 'metadata', 'entry', 'diagnostic', 'format'])
def test_import_evidence_rejects_inconsistent_frozen_scope(change):
    args = evidence_arguments()
    if change == 'workflow':
        args['workflow_id'] = uuid4()
    elif change == 'plan_ref':
        args['plan_ref'].artifact_id = uuid4()
    elif change == 'plan_hash':
        args['plan_ref'].sha256 = '0'*64
    elif change == 'metadata':
        args['metadata'].entries[0].rating = 5
    elif change == 'entry':
        args['sources'].entries[1].ordinal = 0
    elif change == 'format':
        args['plan'].format = 'xmp'
        args['plan_ref'].sha256 = sha256(canonical_bytes(args['plan'])).hexdigest()
    else:
        args['recipe_diagnostics'] = {uuid4(): 'unrelated'}
    with pytest.raises(AdoptionInvalid):
        freeze_import_evidence(**args)


def test_import_evidence_reader_checks_physical_sha_and_outer_scope():
    args = evidence_arguments();prepared = freeze_import_evidence(**args)
    scope = {key: args[key] for key in ('workflow_id', 'plan_ref', 'plan', 'parsed_jcs')}
    with pytest.raises(AdoptionInvalid, match='HASH'):
        read_import_evidence(prepared.reference, b'{}', **scope)
    import json
    wire = json.loads(prepared.body);wire['project_id'] = str(uuid4());raw = canonical_bytes(wire)
    ref = prepared.reference.model_copy(deep=True);ref.sha256 = sha256(raw).hexdigest()
    with pytest.raises(AdoptionInvalid, match='CHANGED'):
        read_import_evidence(ref, raw, **scope)
