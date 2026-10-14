"""Import adoption must reach its dedicated atomic SQL commit."""

from contextlib import nullcontext
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2 import adoption_service, interchange_import_adoption_prepare
from photo_worker.v2.adoption import PreparedAdoption
from photo_worker.v2.interchange_import_adoption import verify_import_output
from test_interchange_import_adoption import adoption_fixture


def test_coordinator_routes_import_to_dedicated_atomic_commit(monkeypatch):
    args, _ = adoption_fixture()
    proof = verify_import_output(**args)
    tenant, task, result = uuid4(), args['task_id'], uuid4()
    inventory = ({'artifact_id': str(proof.root_id), 'sha256': proof.root_sha256,
                  'purpose': 'a' * 64, 'ordinal': 0},)
    prepared = PreparedAdoption(inventory,
        sha256(canonical_bytes(list(inventory))).hexdigest())
    events = []

    class Read:
        autocommit = True

        def execute(self, query, _args):
            if 'v30_interchange_import_adoption_committed' in query:
                return SimpleNamespace(fetchone=lambda: (False,))
            assert 'v2_adoption_context' in query
            return SimpleNamespace(fetchone=lambda: (1, 7, result,
                'v2.interchange.import', '1', proof.root_id, proof.root_sha256))

    class Write:
        def transaction(self):
            return nullcontext()

        def execute(self, query, params):
            assert query.startswith('SELECT photo.adopt_v30_interchange_import(')
            assert len(params) == 12 and params[:5] == (tenant, task, 1, 7, result)
            events.append('dedicated_commit')
            return SimpleNamespace(fetchone=lambda: (True,))

    monkeypatch.setattr(adoption_service, 'PostgresObjectEvidenceReader',
                        lambda *_args, **_kwargs: object())
    monkeypatch.setattr(interchange_import_adoption_prepare,
                        'prepare_current_interchange_import',
                        lambda *_args, **_kwargs: proof)
    monkeypatch.setattr(adoption_service, 'prepare_v2_adoption',
                        lambda **_kwargs: prepared)
    coordinator = adoption_service.AdoptionCoordinator(Read(), Write(), object(), 'photos')
    assert coordinator.adopt(tenant, task) is True
    assert events == ['dedicated_commit']
