"""Current authority and evidence boundary tests; not executed in implementation phase."""
from copy import deepcopy
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactEvidence, ArtifactRef, AdoptionInvalid
from photo_worker.v2.interchange_import_adoption_prepare import prepare_current_interchange_import
from test_interchange_import_adoption import adoption_fixture


class Connection:
    autocommit = True
    def __init__(self, rows):
        self.rows = iter(rows)
        self.calls = []
    def execute(self, query, values):
        self.calls.append((query, values))
        return SimpleNamespace(fetchone=lambda: next(self.rows))


def fixture(mode='valid'):
    args, outputs = adoption_fixture()
    authority = dict(params=args['params_ref'].model_dump(mode='json'),
        workflow_id=str(args['workflow_id']), project_id=str(args['project_id']),
        snapshot_digest=args['snapshot_digest'], max_output_bytes=args['max_output_bytes'])
    after = deepcopy(authority)
    if mode == 'changed':
        after['max_output_bytes'] -= 1
    db = Connection([None] if mode == 'initial' else [(authority,), None if mode == 'revoked' else (after,)])
    roles = {'InterchangeResult': 'document', 'MappingReport': 'report', 'MappingShard': 'mapping_shard'}
    staged = {ArtifactRef(ref.artifact_id, ref.sha256): roles[contract] for ref, contract in args['staged_entries']}
    class Reader:
        reads = 0
        def staged_outputs(self):
            return set(staged)
        def evidence(self, pin):
            source = 'output' if pin in staged else 'input'
            raw = outputs[pin.artifact_id] if source == 'output' else (
                args['params_jcs'] if pin.artifact_id == args['params_ref'].artifact_id else args['read_input'](pin, 8 << 20))
            if mode == 'foreign-source' and source == 'output':
                source = 'input'
            return ArtifactEvidence(pin, 'application/json', len(raw), staged.get(pin), source)
        def read_json(self, pin, maximum):
            self.reads += 1
            if pin in staged:
                return outputs[pin.artifact_id]
            if pin.artifact_id == args['params_ref'].artifact_id:
                return args['params_jcs']
            return args['read_input'](pin, maximum)
    ctx = SimpleNamespace(operation='v2.interchange.import', version='1', attempt=1,
        fence=7, result_id=uuid4(), root=ArtifactRef(args['root'].artifact_id, args['root'].sha256))
    return db, Reader(), uuid4(), args['task_id'], ctx


@pytest.mark.parametrize('mode', ['valid', 'initial', 'revoked', 'changed', 'foreign-source'])
def test_import_preparation_rechecks_current_sql_authority(mode):
    db, reader, tenant, task, ctx = fixture(mode)
    if mode == 'valid':
        proof = prepare_current_interchange_import(db, reader, tenant, task, ctx)
        assert proof.root_id == ctx.root.artifact_id and len(db.calls) == 2
    else:
        with pytest.raises(AdoptionInvalid):
            prepare_current_interchange_import(db, reader, tenant, task, ctx)
        if mode == 'initial':
            assert reader.reads == 0
    assert all(call[1] == (tenant, task, 1, 7, ctx.result_id) for call in db.calls)


def test_import_preparation_refuses_object_io_with_open_transaction():
    db, reader, tenant, task, ctx = fixture()
    db.autocommit = False
    with pytest.raises(ValueError, match='autocommit'):
        prepare_current_interchange_import(db, reader, tenant, task, ctx)
    assert db.calls == [] and reader.reads == 0


def test_import_preparation_cancellation_prevents_sql():
    db, reader, tenant, task, ctx = fixture()
    def cancelled():
        raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError, match='cancelled'):
        prepare_current_interchange_import(db, reader, tenant, task, ctx, check_active=cancelled)
    assert db.calls == []


def test_import_preparation_false_activity_prevents_sql():
    db, reader, tenant, task, ctx = fixture()
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        prepare_current_interchange_import(db, reader, tenant, task, ctx,
                                           check_active=lambda: False)
    assert db.calls == [] and reader.reads == 0


def test_import_evidence_false_activity_prevents_object_read():
    from photo_worker.v2.interchange_import_adoption_prepare import read_import_evidence
    from photo_worker.contracts.base import ArtifactRef as WireRef
    class Reader:
        def evidence(self, _ref):
            raise AssertionError('cancelled adoption read object evidence')
    ref = WireRef(artifact_id=uuid4(), sha256='a'*64)
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        read_import_evidence(Reader(), ref, 1024, 'input', check_active=lambda: False)


@pytest.mark.parametrize('mode', ['success', 'output', 'oversize', 'wrong_hash', 'short', 'overrun', 'cancel'])
def test_opaque_xmp_adoption_reads_are_bounded_scoped_and_closed(mode):
    from hashlib import sha256
    from photo_worker.v2.interchange_import_adoption_prepare import read_import_evidence
    from photo_worker.contracts.base import ArtifactRef as WireRef
    raw = b'<xmp>untrusted pinned XML</xmp>'
    ref = WireRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    closed, opened = [], []
    cancelled = False

    def active():
        if cancelled:
            raise RuntimeError('lease lost')

    class Reader:
        def evidence(self, pin):
            return ArtifactEvidence(pin, 'application/octet-stream', len(raw), None, 'output' if mode == 'output' else 'input')

        def iter_bytes(self, pin, maximum):
            nonlocal cancelled
            opened.append(pin)
            try:
                if mode == 'cancel':
                    cancelled = True
                if mode == 'wrong_hash':
                    yield b'x' * len(raw)
                elif mode == 'short':
                    yield raw[:-1]
                elif mode == 'overrun':
                    yield raw + b'x'
                else:
                    yield raw[:5]
                    yield raw[5:]
            finally:
                closed.append(True)

        def read_json(self, *args):
            pytest.fail('opaque XMP read as JSON')

    source = 'output' if mode == 'output' else 'input'
    maximum = (1 << 20) + 1 if mode == 'oversize' else 1 << 20
    if mode == 'success':
        assert read_import_evidence(Reader(), ref, maximum, source, check_active=active) == raw
    else:
        with pytest.raises(RuntimeError if mode == 'cancel' else AdoptionInvalid):
            read_import_evidence(Reader(), ref, maximum, source, check_active=active)
    assert len(opened) == int(mode not in ('output', 'oversize'))
    assert len(closed) == len(opened)
