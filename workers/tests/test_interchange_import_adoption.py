"""Authored independent adoption checks; execution deferred by user."""
import json
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.interchange_frozen_import import prepare_import_from_params
from photo_worker.v2.interchange_import_adoption import verify_import_output


def test_import_output_false_activity_prevents_physical_read():
    args, _outputs = adoption_fixture()
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        verify_import_output(**args, check_active=lambda: False)
from test_interchange_import_adapter import adapter_fixture


def adoption_fixture(change=None, *, sidecar=False):
    ctx, _, admission, inputs, docs = adapter_fixture(sidecar=sidecar)
    authority = admission.admit(ctx, None)
    outputs, entries = {}, []
    def publish(raw, contract, ordinal):
        ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
        outputs[ref.artifact_id] = raw
        entries.append((ref, contract))
        return ref
    result = prepare_import_from_params(params_ref=docs.params_ref, params_jcs=docs.params_jcs,
        task_id=ctx.lease.command.task_id, workflow_id=authority.workflow_id,
        project_id=authority.project_id, snapshot_digest=authority.snapshot_digest,
        read_document=lambda ref, limit: inputs[ref.artifact_id],
        publish_document=publish,
        publish_root=lambda document: publish(canonical_bytes(document), 'InterchangeResult', 0))
    root = result.root
    if change in ('mapping', 'counter', 'imported', 'drafts', 'extra'):
        shard, report, root_entry = entries
        shard_wire = json.loads(outputs[shard[0].artifact_id])
        if change == 'mapping':
            shard_wire['entries'][0]['fields'][0].update(status='unsupported', reason='invented_reason')
        raw = canonical_bytes(shard_wire)
        shard_ref = ArtifactRef(artifact_id=shard[0].artifact_id, sha256=sha256(raw).hexdigest())
        outputs[shard_ref.artifact_id] = raw
        report_wire = json.loads(outputs[report[0].artifact_id])
        report_wire['shards'] = [shard_ref.model_dump(mode='json')]
        if change == 'counter':
            report_wire['rejected_count'] += 1
        raw = canonical_bytes(report_wire)
        report_ref = ArtifactRef(artifact_id=report[0].artifact_id, sha256=sha256(raw).hexdigest())
        outputs[report_ref.artifact_id] = raw
        root_wire = json.loads(outputs[root.artifact_id])
        root_wire.update(report=report_ref.model_dump(mode='json'), draft_shards=[shard_ref.model_dump(mode='json')])
        if change == 'imported':
            root_wire['imported_files'] = []
        if change == 'drafts':
            root_wire['draft_shards'] = []
        raw = canonical_bytes(root_wire)
        root = ArtifactRef(artifact_id=root.artifact_id, sha256=sha256(raw).hexdigest())
        outputs[root.artifact_id] = raw
        entries = [(shard_ref, 'MappingShard'), (report_ref, 'MappingReport'), (root, 'InterchangeResult')]
        if change == 'extra':
            publish(b'{}', 'MappingShard', 1)
    args = dict(root=root, staged_entries=entries, params_ref=docs.params_ref,
        params_jcs=docs.params_jcs, task_id=ctx.lease.command.task_id,
        workflow_id=authority.workflow_id, project_id=authority.project_id,
        snapshot_digest=authority.snapshot_digest,
        read_input=lambda ref, limit: inputs[ref.artifact_id],
        read_output=lambda ref, limit: outputs[ref.artifact_id], max_output_bytes=1 << 26)
    return args, outputs


def test_import_adoption_independently_reconstructs_all_outputs():
    args, outputs = adoption_fixture()
    proof = verify_import_output(**args)
    assert proof.root_jcs == outputs[args['root'].artifact_id]
    assert len(proof.staged_pins) == 3 and proof.proposal_pins == ()
    assert len(proof.shard_bodies) == 1


@pytest.mark.parametrize('change', ['mapping', 'counter', 'imported', 'drafts', 'extra'])
def test_import_adoption_rejects_rehashed_worker_claims(change):
    args, _ = adoption_fixture(change)
    with pytest.raises((AdoptionInvalid, ValueError)):
        verify_import_output(**args)


def test_import_adoption_bounds_output_reads_before_loading_objects():
    args, _ = adoption_fixture()
    args['max_output_bytes'] = 1
    args['read_output'] = lambda *unused: pytest.fail('budget preflight must reject before IO')
    with pytest.raises(AdoptionInvalid, match='OUTPUT_BUDGET'):
        verify_import_output(**args)


def test_import_adoption_rejects_duplicate_inventory_before_io():
    args, _ = adoption_fixture()
    args['staged_entries'] = args['staged_entries'] * 2
    args['read_output'] = lambda *unused: pytest.fail('duplicate inventory reached IO')
    with pytest.raises(AdoptionInvalid, match='INVENTORY'):
        verify_import_output(**args)


def test_import_adoption_does_not_swallow_cancellation():
    args, _ = adoption_fixture()
    class Cancelled(Exception):
        pass
    args['check_active'] = lambda: (_ for _ in ()).throw(Cancelled())
    with pytest.raises(Cancelled):
        verify_import_output(**args)


def test_import_adoption_rejects_staged_params_alias_before_output_io():
    args, _ = adoption_fixture()
    args['staged_entries'].append((args['params_ref'], 'MappingShard'))
    args['read_output'] = lambda *unused: pytest.fail('input/output alias reached IO')
    with pytest.raises(AdoptionInvalid, match='INPUT_OUTPUT_ALIAS'):
        verify_import_output(**args)


def test_import_adoption_checks_input_bytes_independently():
    args, _ = adoption_fixture()
    args['read_input'] = lambda *unused: b'{}'
    with pytest.raises(AdoptionInvalid, match='INPUT_HASH'):
        verify_import_output(**args)


def test_import_adoption_reconstructs_xmp_report_from_pinned_source_bytes():
    args, outputs = adoption_fixture(sidecar=True)
    proof = verify_import_output(**args)
    entries = json.loads(proof.shard_bodies[0])['entries']
    field = next(field for field in entries[0]['fields'] if field['name']=='xmp_properties')
    assert field['status']=='unsupported' and field['reason'].startswith('unsupported_xmp_properties:1:')
    assert proof.root_jcs == outputs[args['root'].artifact_id]


def test_import_adoption_rejects_changed_xmp_before_accepting_worker_report():
    args, _ = adoption_fixture(sidecar=True)
    original = args['read_input']
    def read(ref, limit):
        raw = original(ref, limit)
        return raw + b'changed' if b'<x:xmpmeta' in raw else raw
    args['read_input'] = read
    with pytest.raises(AdoptionInvalid):
        verify_import_output(**args)
