"""Missing import operator authority must wait, not reject a valid result."""

from threading import Event
from types import SimpleNamespace
from uuid import uuid4

from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.service import Adopter


def test_uninstalled_import_qualifier_is_retryable_without_rejection():
    tenant, run, task = uuid4(), uuid4(), uuid4()
    queries = []

    class Read:
        def execute(self, query, _args=()):
            queries.append(query)
            if 'v2_pending_adoptions' in query:
                return SimpleNamespace(fetchall=lambda: [(tenant, run, task)])
            if 'pg_try_advisory_lock' in query:
                return SimpleNamespace(fetchone=lambda: (True,))
            if 'v2_pending_finalizations' in query:
                return SimpleNamespace(fetchall=lambda: [])
            assert 'pg_advisory_unlock' in query
            return SimpleNamespace(fetchone=lambda: (True,))

    class Coordinator:
        def adopt(self, *_args):
            raise AdoptionInvalid('INTERCHANGE_IMPORT_ADOPTION_INSTALLATION_REQUIRED')

        def context(self, *_args):
            raise AssertionError('retryable installation error reached terminal rejection')

    adopter = object.__new__(Adopter)
    adopter.stop = Event()
    adopter.read = Read()
    adopter.write = object()
    adopter.coordinator = Coordinator()
    adopter.exposure_enabled = False
    for name in ('delivery_encode', 'negative_review_publisher', 'derivative_coordinator',
                 'multi_quality_runner', 'multi_preview_runner', 'restoration_preview_runner',
                 'perspective_preview_runner', 'perspective_quality_runner'):
        setattr(adopter, name, None)
    assert adopter.run_once() == {'adopted': 0, 'rejected': 0, 'finalized': 0, 'retry': 1}
    assert not any('reject_v2_adoption' in query for query in queries)
