"""Coordinator uses only own installation and dedicated locked commit."""
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4

import pytest
from psycopg.errors import DeadlockDetected, SerializationFailure, CheckViolation

from photo_worker.v2 import adoption_service, shown_retouch_commit
from photo_worker.v2.adoption import AdoptionInvalid, ArtifactRef
from photo_worker.v2.shown_retouch_installation import ShownRetouchAdoptionInstallation


@pytest.mark.parametrize('mode', ['ok', 'unconfigured', 'wrong_installation', 'proof_refused', 'commit_refused',
    'inactive_after_proof','inactive_in_transaction','inactive_after_sql',
    'retry_deadlock','retry_serialization','retry_exhausted','nonretryable'])
def test_shown_retouch_coordinator_scopes_prepare_and_commit(monkeypatch, mode):
    tenant, task, result, root, params = [uuid4() for _ in range(5)]
    events = []
    active = True
    attempts = []
    admitted = lambda raw: True
    installation = ShownRetouchAdoptionInstallation(ArtifactRef(params, 'b'*64), admitted)
    additions = (params, 'b'*64, '{}', root, 'a'*64, '{}', '{}')

    class Read:
        autocommit = True
        def execute(self, query, args):
            assert args == (tenant, task)
            if 'editor_effect_seal_adoption_context' in query:
                return SimpleNamespace(fetchone=lambda:None)
            if 'v2_adoption_context' in query:
                row = (1, 2, result, 'semantic.adopt_shown_retouch_region', '1', root, 'a'*64) if mode == 'unconfigured' else None
            else:
                assert 'v27_shown_retouch_adoption_context' in query
                row = (1, 2, result, 'semantic.adopt_shown_retouch_region', '1', root, 'a'*64)
            return SimpleNamespace(fetchone=lambda: row)

    class Write:
        @contextmanager
        def transaction(self):
            nonlocal active
            events.append('transaction')
            if mode=='inactive_in_transaction':active=False
            try:
                yield
            except BaseException:
                events.append('rollback')
                raise
        def execute(self, query, args):
            nonlocal active
            assert query == 'SELECT photo.adopt_shown_retouch('+','.join(['%s']*14)+')'
            assert len(args) == 14 and args[:5] == (tenant, task, 1, 2, result)
            assert args[6] == 'c'*64 and args[7:] == additions
            attempts.append(args)
            assert args is attempts[0]  # retry cannot substitute a newly prepared proof
            events.append('commit')
            if mode=='retry_deadlock' and len(attempts)==1:raise DeadlockDetected('fixture')
            if mode=='retry_serialization' and len(attempts)==1:raise SerializationFailure('fixture')
            if mode=='retry_exhausted':raise SerializationFailure('fixture')
            if mode=='nonretryable':raise CheckViolation('fixture')
            if mode=='inactive_after_sql':active=False
            return SimpleNamespace(fetchone=lambda: (mode != 'commit_refused',))

    def install(actual_tenant, actual_task, context, reader):
        assert (actual_tenant, actual_task, context.result_id) == (tenant, task, result)
        events.append('install')
        return object() if mode == 'wrong_installation' else installation

    def prepare(**kwargs):
        assert kwargs['operation'] == 'semantic.adopt_shown_retouch_region'
        assert kwargs['params_ref'] == installation.params
        assert kwargs['shown_retouch_admission'] is admitted
        assert kwargs['shown_background_cleanup_admission'] is None
        events.append('prepare')
        return SimpleNamespace(inventory=(), digest='c'*64)

    def commit_inputs(reader, installed, supplied_root, *, check_active):
        nonlocal active
        assert installed is installation and supplied_root == ArtifactRef(root, 'a'*64)
        events.append('proof')
        if mode == 'proof_refused': raise AdoptionInvalid('SEMANTIC_SHOWN_ADMISSION_DENIED')
        if mode=='inactive_after_proof':active=False
        return additions

    monkeypatch.setattr(adoption_service, 'prepare_v2_adoption', prepare)
    monkeypatch.setattr(shown_retouch_commit, 'shown_retouch_commit_inputs', commit_inputs)
    monkeypatch.setattr(adoption_service.time,'sleep',lambda seconds:None)
    coordinator = adoption_service.AdoptionCoordinator(Read(), Write(), object(), 'bucket',
        shown_retouch_installation=None if mode == 'unconfigured' else install,
        check_active=lambda:active)
    if mode in ('unconfigured', 'wrong_installation', 'proof_refused'):
        with pytest.raises(AdoptionInvalid): coordinator.adopt(tenant, task)
        assert 'commit' not in events and 'transaction' not in events
    elif mode.startswith('inactive_'):
        with pytest.raises(AdoptionInvalid,match='ADOPTION_INACTIVE'):
            coordinator.adopt(tenant,task)
        if mode=='inactive_after_proof':
            assert events==['install','prepare','proof']
        elif mode=='inactive_in_transaction':
            assert events==['install','prepare','proof','transaction','rollback']
        else:
            assert events==['install','prepare','proof','transaction','commit','rollback']
    elif mode in ('retry_deadlock','retry_serialization'):
        assert coordinator.adopt(tenant,task) is True
        assert events==['install','prepare','proof','transaction','commit','rollback','transaction','commit']
        assert len(attempts)==2
    elif mode=='retry_exhausted':
        with pytest.raises(AdoptionInvalid,match='RETRYABLE_CONFLICT'):coordinator.adopt(tenant,task)
        assert events==['install','prepare','proof']+['transaction','commit','rollback']*3
        assert len(attempts)==3
    elif mode=='nonretryable':
        with pytest.raises(CheckViolation):coordinator.adopt(tenant,task)
        assert events==['install','prepare','proof','transaction','commit','rollback']
        assert len(attempts)==1
    else:
        assert coordinator.adopt(tenant, task) is (mode == 'ok')
        assert events == ['install', 'prepare', 'proof', 'transaction', 'commit']
