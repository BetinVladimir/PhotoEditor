from photo_worker.contracts.images import MaskRef
"""Current qualification must match each imported model-bearing document."""

from copy import deepcopy
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, Contract
from photo_worker.contracts.restoration import DenoiseParams
from photo_worker.contracts.statistics import StatsParams
from photo_worker.operations.session.contracts import VersionBinding
from photo_worker.v2.interchange_import_adoption_runtime import (
    ImportAdoptionQualification, SchedulerImportAdoptionRuntimeAuthority)
from photo_worker.v2.adoption import AdoptionInvalid
from test_interchange_import_adoption_qualification_report import fixture as report_fixture
from test_interchange_person_mask_confirmation import _mask_and_binding


def setup(expected, *, other=lambda *_args: True):
    calls = []
    class Catalog:
        def verified_report(self, _objects, scope, operation, policy, report, digest):
            calls.append((scope, operation, policy, report, digest))
            return {'operation': operation, 'model_lock_sha': policy['ModelLockSHA']}
    objects = SimpleNamespace(get_object=lambda *_args, **_kwargs: None)
    chosen = ImportAdoptionQualification(expected, uuid4(), 'f' * 64)
    authority = SchedulerImportAdoptionRuntimeAuthority(
        Catalog(), objects, {'image.denoise': chosen}, other, lambda: None)
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    context = SimpleNamespace(operation='v2.interchange.import', version='1',
                              attempt=1, fence=7, result_id=scope[4])
    return authority, scope, context, calls


def test_import_runtime_binds_denoise_params_to_current_model_report():
    expected, _ = report_fixture()
    authority, scope, context, calls = setup(expected)
    binding = VersionBinding(asset_version_id=uuid4(),
        source=ArtifactRef(artifact_id=uuid4(), sha256='a' * 64),
        canvas_hash='b' * 64)
    params = DenoiseParams(image=binding.source, strength=0.5,
                           model_lock_id=expected['ModelLockSHA'])
    assert authority(scope, context, 'Params', params, binding) is True
    assert len(calls) == 1 and calls[0][1] == 'image.denoise'
    assert authority(scope, context, 'Params',
        params.model_copy(update={'model_lock_id': '0' * 64}), binding) is False
    assert len(calls) == 1


def test_import_runtime_requires_independent_semantics_and_complete_expected():
    expected, _ = report_fixture()
    authority, scope, context, _ = setup(expected, other=lambda *_args: False)
    binding = VersionBinding(asset_version_id=uuid4(),
        source=ArtifactRef(artifact_id=uuid4(), sha256='a' * 64),
        canvas_hash='b' * 64)
    params = DenoiseParams(image=binding.source, strength=0.5,
                           model_lock_id=expected['ModelLockSHA'])
    assert authority(scope, context, 'Params', params, binding) is False
    invalid = deepcopy(expected)
    invalid.pop('EvidenceSHAs')
    with pytest.raises(ValueError, match='QUALIFICATION'):
        setup(invalid)
    unavailable = SchedulerImportAdoptionRuntimeAuthority(
        SimpleNamespace(verified_report=lambda *_args: None),
        SimpleNamespace(get_object=lambda *_args, **_kwargs: None),
        {}, lambda *_args: True, lambda: None)
    with pytest.raises(AdoptionInvalid, match='UNAVAILABLE'):
        unavailable(scope, context, 'Params', params, binding)


def test_import_runtime_stats_requires_separate_person_model_report():
    expected, _ = report_fixture()
    stats = deepcopy(expected)
    stats['Operation'], stats['ModelLockSHA'] = 'color.stats', 'e' * 64
    person = deepcopy(expected)
    person['Operation'], person['ModelLockSHA'] = 'mask.person', 'd' * 64
    calls = []
    class Catalog:
        def verified_report(self, _objects, _scope, operation, policy, *_args):
            calls.append(operation)
            return {'operation': operation, 'model_lock_sha': policy['ModelLockSHA']}
    authority = SchedulerImportAdoptionRuntimeAuthority(
        Catalog(), SimpleNamespace(get_object=lambda *_args, **_kwargs: None),
        {'color.stats': ImportAdoptionQualification(stats, uuid4(), 'a' * 64),
         'mask.person': ImportAdoptionQualification(person, uuid4(), 'b' * 64)},
        lambda *_args: True, lambda: None, lambda *_args: 7)
    mask, binding = _mask_and_binding()
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    context = SimpleNamespace(operation='v2.interchange.import', version='1',
                              attempt=1, fence=7, result_id=scope[4])
    assert authority(scope, context, 'Params',
                     StatsParams(image=binding.source, person=mask), binding) is True
    assert calls == ['mask.person', 'color.stats']
    scoped = mask.model_copy(update={'object_id': uuid4()})
    assert authority(scope, context, 'MaskRef', scoped, binding) is False
    assert authority(scope, context, 'Params',
                     StatsParams(image=binding.source, person=scoped), binding) is False
    assert calls == ['mask.person', 'color.stats']


def test_import_runtime_checks_person_masks_nested_in_other_documents():
    expected, _ = report_fixture()
    person = deepcopy(expected)
    person['Operation'], person['ModelLockSHA'] = 'mask.person', 'd' * 64
    calls = []
    class Catalog:
        def verified_report(self, _objects, _scope, operation, policy, *_args):
            calls.append(operation)
            return {'operation': operation, 'model_lock_sha': policy['ModelLockSHA']}
    class Nested(Contract):
        masks: list[MaskRef]
    authority = SchedulerImportAdoptionRuntimeAuthority(
        Catalog(), SimpleNamespace(get_object=lambda *_args, **_kwargs: None),
        {'mask.person': ImportAdoptionQualification(person, uuid4(), 'a' * 64)},
        lambda *_args: True, lambda: None, lambda *_args: 7)
    mask, binding = _mask_and_binding()
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    context = SimpleNamespace(operation='v2.interchange.import', version='1',
                              attempt=1, fence=7, result_id=scope[4])
    assert authority(scope, context, 'Override', Nested(masks=[mask, mask]), binding) is True
    assert calls == ['mask.person']
    calls.clear()
    scoped = mask.model_copy(update={'object_id': uuid4()})
    assert authority(scope, context, 'Override', Nested(masks=[scoped]), binding) is False
    assert authority(scope, context, 'Override', Nested(masks=[scoped, mask]), binding) is False
    assert calls == []


def test_import_runtime_rechecks_person_grant_after_report_io():
    expected, _ = report_fixture()
    expected['Operation'], expected['ModelLockSHA'] = 'mask.person', 'd' * 64
    mask, binding = _mask_and_binding()
    epochs = iter((7, 8))
    authority = SchedulerImportAdoptionRuntimeAuthority(
        SimpleNamespace(verified_report=lambda *_args: {
            'operation': 'mask.person', 'model_lock_sha': 'd' * 64}),
        SimpleNamespace(get_object=lambda *_args, **_kwargs: None),
        {'mask.person': ImportAdoptionQualification(expected, uuid4(), 'a' * 64)},
        lambda *_args: True, lambda: None, lambda *_args: next(epochs))
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    context = SimpleNamespace(operation='v2.interchange.import', version='1',
                              attempt=1, fence=7, result_id=scope[4])
    with pytest.raises(AdoptionInvalid, match='PERSON_AUTHORITY_CHANGED'):
        authority(scope, context, 'MaskRef', mask, binding)
    with pytest.raises(ValueError, match='PERSON_AUTHORITY'):
        SchedulerImportAdoptionRuntimeAuthority(
            SimpleNamespace(verified_report=lambda *_args: None),
            SimpleNamespace(get_object=lambda *_args, **_kwargs: None),
            {'mask.person': ImportAdoptionQualification(expected, uuid4(), 'a' * 64)},
            lambda *_args: True, lambda: None)


def test_import_runtime_scope_rechecks_every_used_person_mask_at_exit():
    expected, _ = report_fixture()
    expected['Operation'], expected['ModelLockSHA'] = 'mask.person', 'd' * 64
    mask, binding = _mask_and_binding()
    epochs = iter((7, 7, 8))
    authority = SchedulerImportAdoptionRuntimeAuthority(
        SimpleNamespace(verified_report=lambda *_args: {
            'operation': 'mask.person', 'model_lock_sha': 'd' * 64}),
        SimpleNamespace(get_object=lambda *_args, **_kwargs: None),
        {'mask.person': ImportAdoptionQualification(expected, uuid4(), 'a' * 64)},
        lambda *_args: True, lambda: None, lambda *_args: next(epochs))
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    context = SimpleNamespace(operation='v2.interchange.import', version='1',
                              attempt=1, fence=7, result_id=scope[4])
    qualify, recheck = authority.bound_scope(scope, context)
    assert qualify('MaskRef', mask, binding) is True
    with pytest.raises(AdoptionInvalid, match='PERSON_AUTHORITY_CHANGED'):
        recheck()


@pytest.mark.parametrize('operation', ['defects.auto_repair', 'sensor.dust_auto_repair'])
@pytest.mark.parametrize('mode', ['valid', 'missing_report', 'semantic_deny', 'foreign_source', 'wrong_backend'])
def test_conservative_import_requires_cpu_current_measured_report_and_independent_semantics(operation, mode):
    from photo_worker.contracts.conservative import DefectAutoRepairParams, DustAutoRepairParams
    expected, _ = report_fixture(); expected = deepcopy(expected)
    expected.update(Operation=operation, Backend='cuda' if mode == 'wrong_backend' else 'cpu')
    calls = []
    class Catalog:
        def verified_report(self, objects, scope, name, policy, report, digest):
            calls.append(name)
            return {'operation': name, 'model_lock_sha': policy['ModelLockSHA']}
    selected = ImportAdoptionQualification(expected, uuid4(), 'f'*64)
    kwargs = dict(catalog=Catalog(), objects=SimpleNamespace(get_object=lambda *args, **kwargs: None),
        qualifications={} if mode == 'missing_report' else {operation: selected},
        qualify_other=lambda *args: mode != 'semantic_deny', check_active=lambda: None)
    if mode == 'wrong_backend':
        with pytest.raises(ValueError, match='QUALIFICATION'): SchedulerImportAdoptionRuntimeAuthority(**kwargs)
        return
    authority = SchedulerImportAdoptionRuntimeAuthority(**kwargs)
    mask, binding = _mask_and_binding()
    source = ArtifactRef(artifact_id=uuid4(), sha256='a'*64) if mode == 'foreign_source' else binding.source
    if operation == 'defects.auto_repair':
        params = DefectAutoRepairParams(policy='conservative_auto_v1', image=source,
            proposal=source, proposal_report=source, resolved=source)
    else:
        # Avoid asserting whole-person authority through a permissive test mask.
        mask.selector='manual'; mask.model_lock_id=None; mask.object_id=None; mask.protected=True
        params = DustAutoRepairParams(policy='conservative_auto_v1', image=source, proposal=None, resolved=source, protected=[mask])
    scope = (uuid4(), uuid4(), 1, 7, uuid4())
    context = SimpleNamespace(operation='v2.interchange.import', version='1', attempt=1, fence=7, result_id=scope[4])
    assert authority(scope, context, 'Params', params, binding) is (mode == 'valid')
    assert calls == ([operation] if mode in ('valid', 'semantic_deny') else [])
