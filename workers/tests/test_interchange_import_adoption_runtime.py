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
        lambda *_args: True, lambda: None)
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
        lambda *_args: True, lambda: None)
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
