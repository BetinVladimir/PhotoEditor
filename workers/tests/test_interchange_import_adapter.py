from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
from uuid import uuid4, uuid5
import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.interchange.import_adapter import ImportAuthority, ImportInstallation, execute_import
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.interchange_import_evidence import freeze_import_evidence
from photo_worker.v2.interchange_task_documents import freeze_import_execution_task_documents
from test_interchange_import_stage import adopted_fixture
from test_interchange_plan_compile import compile


def adapter_fixture(*, sidecar=False):
    parsed, sources, metadata, image = adopted_fixture()
    extra_inputs = {}
    if sidecar:
        import json
        from pathlib import PurePosixPath
        from photo_worker.v2.interchange_xmp import XMPMapping, export_xmp
        value = json.loads(parsed)
        path = str(PurePosixPath(value['manifest']['entries'][0]['image_path']).with_suffix('.xmp'))
        xmp = export_xmp(XMPMapping(0, 'unmarked', 1, None, ()))
        xmp = xmp.replace(b'rdf:about=""', b'rdf:about="" crs:Exposure2012="2"')
        ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(xmp).hexdigest())
        extra_inputs[ref.artifact_id] = xmp
        value['manifest']['inventory'].append({'artifact_id':str(uuid4()),'path':path,'sha256':ref.sha256,'contract':'XMP'})
        value['files'].append({'path':path,'content':ref.model_dump(mode='json'),'contract':'XMP'})
        parsed = canonical_bytes(value)
        sources.parsed.sha256 = sha256(parsed).hexdigest()
        metadata.parsed.sha256 = sources.parsed.sha256
    plan = compile(parsed, sources, lambda *args: image)
    task = uuid5(sources.workflow_id, 'v2.interchange.import:0')
    plan_ref = ArtifactRef(artifact_id=uuid5(task, 'interchange:plan:1'), sha256=sha256(canonical_bytes(plan)).hexdigest())
    evidence = freeze_import_evidence(workflow_id=sources.workflow_id, plan_ref=plan_ref,
        plan=plan, parsed_jcs=parsed, sources=sources, metadata=metadata, recipe_diagnostics={})
    docs = freeze_import_execution_task_documents(sources.workflow_id, sources.parsed, plan, parsed, evidence)
    bodies = {docs.params_id: docs.params_jcs, docs.plan_id: docs.plan_jcs, docs.evidence_id: docs.evidence_jcs,
              sources.parsed.artifact_id: parsed, sources.entries[0].master.artifact_id: image, **extra_inputs}
    command = SimpleNamespace(operation='v2.interchange.import', operation_version='1', resource_class='cpu',
                              task_id=task, params=docs.params_ref)
    ctx = SimpleNamespace(check_active=lambda: None, lease=SimpleNamespace(command=command,
        execution_profile={'backend': 'cpu', 'renderer_version': 'v2.2',
                           'resource_estimate': {'vram_bytes': 0, 'output_bytes': 1 << 26}}))
    authority = ImportAuthority(sources.workflow_id, sources.project_id, 'f'*64, 128 << 20, 1 << 26)
    class Admission:
        calls = 0
        stale_after = None
        def admit(self, ctx, params):
            self.calls += 1
            if self.stale_after is not None and self.calls > self.stale_after:
                return ImportAuthority(authority.workflow_id, uuid4(), authority.snapshot_digest,
                                       authority.max_input_json_bytes, authority.max_output_bytes)
            return authority
    class Artifacts:
        roles = []
        opens = 0
        def params(self, model):
            return model.model_validate_json(docs.params_jcs)
        def require_declared_input(self, ref):
            assert ref.artifact_id in bodies
        @contextmanager
        def stream(self, ref):
            self.opens += 1
            yield BytesIO(bodies[ref.artifact_id])
        def stage_contract(self, document, role):
            self.roles.append(role)
            return ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(document)).hexdigest())
    return ctx, Artifacts(), Admission(), bodies, docs


def test_leased_import_adapter_stages_only_report_shards_and_root():
    ctx, artifacts, admission, _, _ = adapter_fixture()
    result = execute_import(ctx, artifacts, ImportInstallation(admission))
    assert artifacts.roles == ['mapping_shard', 'report', 'document']
    assert len(result.outputs) == 3 and result.review_required is False
    assert result.peak_device_bytes == 0 and result.bytes_read > 0 and result.bytes_written > 0
    assert admission.calls >= 8


def test_leased_import_adapter_rechecks_authority_after_first_publication():
    ctx, artifacts, admission, _, _ = adapter_fixture();admission.stale_after = 3
    with pytest.raises(OperationFailure, match='INTERCHANGE_SOURCE_STALE'):
        execute_import(ctx, artifacts, ImportInstallation(admission))
    assert artifacts.roles == ['mapping_shard']


def test_leased_import_adapter_rejects_changed_evidence_before_publication():
    ctx, artifacts, admission, bodies, docs = adapter_fixture();bodies[docs.evidence_id] = b'{}'
    with pytest.raises(OperationFailure, match='INPUT_INVALID'):
        execute_import(ctx, artifacts, ImportInstallation(admission))
    assert artifacts.roles == []


def test_leased_import_adapter_requires_cpu_profile_before_input_io():
    ctx, artifacts, admission, _, _ = adapter_fixture();ctx.lease.execution_profile['resource_estimate']['vram_bytes'] = 1
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        execute_import(ctx, artifacts, ImportInstallation(admission))
    assert artifacts.opens == 0 and admission.calls == 0


def test_import_adapter_shares_factory_budget_and_reports_factory_reads():
    ctx, artifacts, admission, _, _ = adapter_fixture()
    baseline = execute_import(ctx, artifacts, ImportInstallation(admission)).bytes_read
    ctx, artifacts, admission, _, _ = adapter_fixture()
    class Factory:
        def __call__(self, *args):
            raise AssertionError('shared budget builder must be used')
        def build_with_read_budget(self, ctx, artifacts, authority, budget):
            budget.consume(7, is_json=True)
            budget.consume(19, is_json=False)
            return lambda *args: True
    result = execute_import(ctx, artifacts, ImportInstallation(admission, Factory()))
    assert result.bytes_read == baseline + 26


def test_import_adapter_cannot_reuse_factory_exhausted_json_budget():
    ctx, artifacts, admission, _, _ = adapter_fixture()
    class Factory:
        def __call__(self, *args):
            raise AssertionError('shared budget builder must be used')
        def build_with_read_budget(self, ctx, artifacts, authority, budget):
            budget.consume(authority.max_input_json_bytes, is_json=True)
            return lambda *args: True
    with pytest.raises(OperationFailure, match='INPUT_INVALID'):
        execute_import(ctx, artifacts, ImportInstallation(admission, Factory()))
    assert artifacts.opens == 0 and artifacts.roles == []


def test_import_adapter_rejects_factory_without_shared_resource_accounting():
    ctx, artifacts, admission, _, _ = adapter_fixture()
    with pytest.raises(OperationFailure, match='READ_BUDGET_REQUIRED'):
        execute_import(ctx, artifacts, ImportInstallation(admission, lambda *args: lambda *args: True))
    assert artifacts.opens == 0 and artifacts.roles == []


def test_leased_import_adapter_reports_pinned_xmp_unsupported_properties():
    ctx, artifacts, admission, bodies, docs = adapter_fixture(sidecar=True)
    staged = []
    original = artifacts.stage_contract
    def stage(document, role):
        staged.append((role, canonical_bytes(document)))
        return original(document, role)
    artifacts.stage_contract = stage
    result = execute_import(ctx, artifacts, ImportInstallation(admission))
    import json
    entries = json.loads(staged[0][1])['entries']
    assert next(field for field in entries[0]['fields'] if field['name']=='xmp_properties')['status']=='unsupported'
    assert len(result.outputs)==3 and artifacts.roles==['mapping_shard','report','document']


@pytest.mark.parametrize('at_call,staged_count', [(1,0),(2,0),(9,3)])
@pytest.mark.parametrize('mode', ['mutation','cancel'])
def test_import_authority_callbacks_cannot_mutate_params_or_return_cancelled_success(at_call, staged_count, mode):
    ctx, artifacts, admission, bodies, docs = adapter_fixture(sidecar=True)
    from photo_worker.v2.interchange_plan import InterchangeImportParams
    caller_params = InterchangeImportParams.model_validate_json(docs.params_jcs)
    original_params = canonical_bytes(caller_params)
    artifacts.params = lambda model:caller_params
    original_admit = admission.admit
    cancelled = False
    calls = 0

    def active():
        if cancelled:
            raise RuntimeError('lease lost')

    def admit(context, candidate):
        nonlocal calls, cancelled
        calls += 1
        authority = original_admit(context, candidate)
        if calls == at_call:
            if mode == 'mutation':
                candidate.parsed.sha256 = 'f' * 64
            else:
                cancelled = True
        return authority

    ctx.check_active = active
    admission.admit = admit
    with pytest.raises(OperationFailure if mode == 'mutation' else RuntimeError):
        execute_import(ctx, artifacts, ImportInstallation(admission))
    assert calls == at_call
    assert len(artifacts.roles) == staged_count
    assert canonical_bytes(caller_params) == original_params
    if at_call <= 2:
        assert artifacts.opens == 0
