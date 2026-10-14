"""Completed-task dependency authority for relight replay.

Qualification verifier must independently validate installed runtime and original
report/calibration bytes against the current policy. This does not commit SQL.
"""
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from .adoption import AdoptionInvalid
from .relight_adoption_source import load_relight_adoption_source, relight_adoption_source_checkpoint
from .relight_adoption_depth import verify_relight_adoption_depth
from .relight_adoption_person import verify_relight_adoption_person
from .relight_adoption_manual import verify_relight_adoption_manual


class CompletedRelightAuthority:
    def __init__(self, connection, reader, tenant_id, task_id, context, *, scratch_root,
                 max_bytes, verify_qualification, check_active):
        if (connection.autocommit is not True or not callable(verify_qualification)
                or not callable(check_active) or type(max_bytes) is not int
                or not 1 <= max_bytes <= 2**53-1):
            raise ValueError('RELIGHT_ADOPTION_AUTHORITY_CONFIGURATION')
        self.connection, self.reader = connection, reader
        self.tenant_id, self.task_id, self.context = tenant_id, task_id, deepcopy(context)
        self.scratch_root, self.max_bytes = scratch_root, max_bytes
        self.qualification, self.check = verify_qualification, check_active

    def __call__(self, params, source, depth, subject, guard, calibration):
        docs = (params, source, depth, subject, guard, calibration)
        identity = canonical_hash([doc.model_dump(mode='json') for doc in docs])
        def check():
            self.check()
            if canonical_hash([doc.model_dump(mode='json') for doc in docs]) != identity:
                raise AdoptionInvalid('RELIGHT_ADOPTION_AUTHORITY_CHANGED')
        check()
        if (canonical_hash(source) != params.base.source.sha256
                or canonical_hash(calibration) != params.calibration.sha256
                or canonical_hash(subject) != params.subject.sha256
                or canonical_hash(guard) != params.protected.sha256
                or canonical_hash(depth) != params.depth.sha256
                or subject.protected or not guard.protected
                or any(doc.canvas != source.canvas or doc.transform_chain != source.transform_chain
                       for doc in (subject, guard, depth))):
            raise AdoptionInvalid('RELIGHT_ADOPTION_AUTHORITY_BINDING')
        # Reject unsupported producers before any expensive replay or object read.
        if subject.selector not in ('manual', 'person') or guard.selector != 'manual':
            raise AdoptionInvalid('RELIGHT_ADOPTION_MASK_UNSUPPORTED')
        args = (self.connection, self.reader, self.tenant_id, self.task_id, self.context, params)
        body = canonical_bytes(params)
        evidence = load_relight_adoption_source(self.connection, self.tenant_id, self.task_id,
            self.context, body, check_active=check)
        checkpoint = relight_adoption_source_checkpoint(self.connection, self.tenant_id,
            self.task_id, self.context, body, evidence, check_active=check)
        def qualification():
            check()
            if self.qualification(deepcopy(evidence), params, calibration) is not True:
                raise AdoptionInvalid('RELIGHT_ADOPTION_QUALIFICATION_REVOKED')
            check(); checkpoint()
        qualification()
        if verify_relight_adoption_depth(*args, depth, check_active=check) is not True:
            raise AdoptionInvalid('RELIGHT_ADOPTION_DEPTH_INVALID')
        for mask, protected in ((subject, False), (guard, True)):
            check()
            if mask.selector == 'manual':
                valid = verify_relight_adoption_manual(*args, mask, protected=protected,
                    scratch_root=self.scratch_root, max_bytes=self.max_bytes, check_active=check)
            else:
                valid = verify_relight_adoption_person(*args, mask, check_active=check)
            if valid is not True:
                raise AdoptionInvalid('RELIGHT_ADOPTION_MASK_INVALID')
        qualification()
        return True


def qualified_completed_relight_authority(connection, reader, tenant_id, task_id, context,
        *, scratch_root, max_bytes, runtime_lock, expected_runtime_sha, check_active):
    """Explicit installed-runtime factory; never auto-qualifies changed code."""
    from photo_worker.operations.portrait.relight_runtime_lock import verify_installed_runtime
    from .relight_adoption_qualification import verify_completed_relight_qualification
    lock = deepcopy(runtime_lock)
    verify_installed_runtime(lock, expected_runtime_sha)
    owned_context = deepcopy(context)
    def qualification(evidence, params, calibration):
        check_active()
        verify_installed_runtime(lock, expected_runtime_sha)
        valid = verify_completed_relight_qualification(connection, reader, tenant_id, task_id,
            owned_context, params, calibration, expected_runtime_sha=expected_runtime_sha,
            check_active=check_active)
        verify_installed_runtime(lock, expected_runtime_sha)
        check_active()
        return valid
    return CompletedRelightAuthority(connection,reader,tenant_id,task_id,owned_context,
        scratch_root=scratch_root,max_bytes=max_bytes,verify_qualification=qualification,
        check_active=check_active)
