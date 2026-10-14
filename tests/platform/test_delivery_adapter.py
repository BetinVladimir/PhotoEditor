"""Adapter protocol tests with explicit lease/admission/artifact doubles."""
import json
from types import SimpleNamespace
from typing import Literal
import pytest
from photo_worker.contracts.base import ArtifactRef, Contract, canonical_bytes
from photo_worker.contracts.delivery import V2DeliveryAssembleParams, V2DeliveryPrepareParams
from photo_worker.operations.delivery.adapter import DeliveryInstallation, delivery_operations, execute
from photo_worker.runtime.executor import OperationFailure
from test_delivery_assembly import fixture as matrix_fixture, ref
from test_delivery_preparation import fixture as prepare_fixture


class PresetDouble(Contract):
    format: Literal['jpeg']


class ContextDouble:
    def __init__(self, operation):
        self.lease = SimpleNamespace(execution_profile={'renderer_version': 'v2.2', 'backend': 'cpu'},
                                    command=SimpleNamespace(operation=operation, operation_version='1', resource_class='cpu'))
        self.checks = 0
    def check_active(self): self.checks += 1


class ArtifactsDouble:
    def __init__(self, params, objects): self.value, self.objects, self.staged = params, objects, []
    def params(self, model):
        assert isinstance(self.value, model)
        return self.value
    def contract_bytes(self, reference, model):
        raw = self.objects[str(reference.artifact_id)]
        model.model_validate_json(raw)
        return raw
    def stage_contract(self, value, role):
        self.staged.append((value, role))
        return ArtifactRef.model_validate_json(json.dumps(ref(90, canonical_bytes(value))))


class AdmissionDouble:
    def __init__(self): self.calls = 0
    def admit(self, ctx, params): self.calls += 1


def test_assembly_adapter_rechecks_admission_and_stages_only_manifest():
    assembly, shard = matrix_fixture()
    shard_raw = canonical_bytes(shard)
    assembly['shards'] = [ref(50, shard_raw)]
    raw = canonical_bytes(assembly)
    params = V2DeliveryAssembleParams.model_validate_json(json.dumps({'schema_version': 1, 'assembly': ref(51, raw)}))
    artifacts = ArtifactsDouble(params, {str(params.assembly.artifact_id): raw, str(assembly['shards'][0]['artifact_id']): shard_raw})
    admission = AdmissionDouble()
    result = execute(ContextDouble('v2.delivery.assemble'), artifacts,
                     DeliveryInstallation(admission, PresetDouble, lambda raw: None), operation='v2.delivery.assemble')
    assert admission.calls == 2
    assert len(artifacts.staged) == len(result.outputs) == 1
    assert artifacts.staged[0][1] == 'document'
    assert result.review_required is False


def test_preparation_adapter_uses_frozen_configs_and_required_validator():
    frozen, objects = prepare_fixture()
    raw = canonical_bytes(frozen)
    params = V2DeliveryPrepareParams.model_validate_json(json.dumps({'schema_version': 1, 'input': ref(6, raw)}))
    objects[str(params.input.artifact_id)] = raw
    artifacts = ArtifactsDouble(params, objects)
    seen = []
    admission = AdmissionDouble()
    result = execute(ContextDouble('v2.delivery.prepare'), artifacts,
                     DeliveryInstallation(admission, PresetDouble, seen.append), operation='v2.delivery.prepare')
    assert len(seen) == 1 and admission.calls == 2
    assert result.bytes_written == len(canonical_bytes(artifacts.staged[0][0]))


def test_second_admission_refusal_cannot_stage_output():
    frozen, objects = prepare_fixture()
    raw = canonical_bytes(frozen)
    params = V2DeliveryPrepareParams.model_validate_json(json.dumps({'schema_version': 1, 'input': ref(6, raw)}))
    objects[str(params.input.artifact_id)] = raw
    artifacts = ArtifactsDouble(params, objects)
    class Revoked(AdmissionDouble):
        def admit(self, ctx, params):
            super().admit(ctx, params)
            if self.calls == 2: raise OperationFailure('INPUT_REVOKED')
    with pytest.raises(OperationFailure):
        execute(ContextDouble('v2.delivery.prepare'), artifacts,
                DeliveryInstallation(Revoked(), PresetDouble, lambda raw: None), operation='v2.delivery.prepare')
    assert artifacts.staged == []


def test_invalid_installation_and_profile_refuse_execution():
    with pytest.raises(ValueError, match='DELIVERY_INSTALLATION_INVALID'):
        delivery_operations(None, None)
    ctx = ContextDouble('v2.delivery.assemble')
    ctx.lease.execution_profile['backend'] = 'mps'
    with pytest.raises(OperationFailure): execute(ctx, None, None, operation='v2.delivery.assemble')

def test_default_installation_validates_production_frozen_preset_contract():
    from test_frozen_export_preset import fixture as preset_fixture
    frozen, objects = prepare_fixture()
    config_raw = canonical_bytes(preset_fixture())
    config_ref = ref(5, config_raw)
    frozen['presets'][0].update(config=config_ref, preset_sha=config_ref['sha256'])
    objects[config_ref['artifact_id']] = config_raw
    raw = canonical_bytes(frozen)
    params = V2DeliveryPrepareParams.model_validate_json(json.dumps({'schema_version': 1, 'input': ref(6, raw)}))
    objects[str(params.input.artifact_id)] = raw
    artifacts = ArtifactsDouble(params, objects)
    result = execute(ContextDouble('v2.delivery.prepare'), artifacts,
                     DeliveryInstallation(AdmissionDouble()), operation='v2.delivery.prepare')
    assert len(result.outputs) == 1
    assert artifacts.staged[0][0].slot_count == 1


@pytest.mark.parametrize('profile', [None, [], 'cpu', True, 1])
@pytest.mark.parametrize('operation', ['v2.delivery.prepare', 'v2.delivery.assemble'])
def test_malformed_profile_refused_before_admission_or_artifact_access(profile, operation):
    ctx = ContextDouble(operation)
    ctx.lease.execution_profile = profile
    admission = AdmissionDouble()
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        execute(ctx, None, DeliveryInstallation(admission), operation=operation)
    assert admission.calls == 0
    assert ctx.checks == 0


@pytest.mark.parametrize('operation', ['v2.delivery.prepare', 'v2.delivery.assemble'])
@pytest.mark.parametrize('mutate_at', [1, 2])
def test_admission_cannot_rebind_owned_params(operation, mutate_at):
    if operation == 'v2.delivery.prepare':
        frozen, objects = prepare_fixture()
        raw = canonical_bytes(frozen)
        params = V2DeliveryPrepareParams.model_validate_json(json.dumps({'schema_version': 1, 'input': ref(6, raw)}))
        objects[str(params.input.artifact_id)] = raw
    else:
        assembly, shard = matrix_fixture()
        shard_raw = canonical_bytes(shard)
        assembly['shards'] = [ref(50, shard_raw)]
        raw = canonical_bytes(assembly)
        params = V2DeliveryAssembleParams.model_validate_json(json.dumps({'schema_version': 1, 'assembly': ref(51, raw)}))
        objects = {str(params.assembly.artifact_id): raw, str(assembly['shards'][0]['artifact_id']): shard_raw}
    original = canonical_bytes(params)
    artifacts = ArtifactsDouble(params, objects)

    class MutatingAdmission(AdmissionDouble):
        def admit(self, ctx, candidate):
            super().admit(ctx, candidate)
            assert candidate is not params
            if self.calls == mutate_at:
                reference = candidate.input if operation == 'v2.delivery.prepare' else candidate.assembly
                reference.sha256 = 'f' * 64

    admission = MutatingAdmission()
    with pytest.raises(OperationFailure, match='INPUT_INVALID'):
        execute(ContextDouble(operation), artifacts,
                DeliveryInstallation(admission, PresetDouble, lambda raw: None), operation=operation)
    assert admission.calls == mutate_at
    assert artifacts.staged == []
    assert canonical_bytes(params) == original


def test_artifact_reader_cannot_change_owned_preparation_reference():
    frozen, objects = prepare_fixture()
    raw = canonical_bytes(frozen)
    params = V2DeliveryPrepareParams.model_validate_json(json.dumps({'schema_version': 1, 'input': ref(6, raw)}))
    expected = params.input.model_copy(deep=True)
    objects[str(params.input.artifact_id)] = raw

    class MutatingArtifacts(ArtifactsDouble):
        def contract_bytes(self, reference, model):
            data = super().contract_bytes(reference, model)
            if model.__name__ == 'FrozenDeliveryInput':
                params.input.sha256 = 'f' * 64
                reference.sha256 = '0' * 64
            return data

    artifacts = MutatingArtifacts(params, objects)
    execute(ContextDouble('v2.delivery.prepare'), artifacts,
            DeliveryInstallation(AdmissionDouble(), PresetDouble, lambda raw: None), operation='v2.delivery.prepare')
    assert artifacts.staged[0][0].input == expected
