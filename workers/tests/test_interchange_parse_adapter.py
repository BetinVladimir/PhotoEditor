from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, Contract, canonical_bytes
from photo_worker.operations.interchange.parse import (
    ImportRequest, InterchangeParseParams, ParseInstallation, execute_parse,
)
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.interchange_archive import ArchiveLimits


def context(params, *, backend="cpu"):
    command = SimpleNamespace(operation="v2.interchange.parse", operation_version="1", resource_class="cpu",
                              params=ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(params)).hexdigest()))
    return SimpleNamespace(check_active=lambda: None, lease=SimpleNamespace(command=command,
        execution_profile={"backend": backend, "renderer_version": "v2.2"}))


def params(format="photoeditor_bundle"):
    return InterchangeParseParams(schema_version=1, request=ImportRequest(project_id=uuid4(), direction="import",
        format=format, bundle=ArtifactRef(artifact_id=uuid4(), sha256="a" * 64)), max_expanded_bytes=10000, max_files=10)


class Artifacts:
    def __init__(self, value):
        self.value, self.roots, self.declared = value, [], []

    def params(self, model):
        assert model is InterchangeParseParams
        return self.value

    def require_declared_input(self, ref):
        self.declared.append(ref)

    def stage_contract(self, document, role):
        self.roots.append((document, role))
        return ArtifactRef(artifact_id=uuid4(), sha256=sha256(canonical_bytes(document)).hexdigest())


class Admission:
    def __init__(self, values):
        self.values = iter(values)

    def admit(self, ctx, value):
        return next(self.values)


def test_wrong_profile_and_params_do_not_parse(monkeypatch):
    monkeypatch.setattr('photo_worker.operations.interchange.parse.stage_photoeditor_bundle', lambda *args: pytest.fail('unexpected IO'))
    value = params()
    with pytest.raises(OperationFailure, match="CONFIG_INVALID"):
        execute_parse(context(value, backend="gpu"), Artifacts(value), ParseInstallation(Admission([])))
    ctx = context(value)
    ctx.lease.command.params.sha256 = "0" * 64
    with pytest.raises(OperationFailure, match="INPUT_INVALID"):
        execute_parse(ctx, Artifacts(value), ParseInstallation(Admission([])))


@pytest.mark.parametrize('format', ['xmp', 'captureone_sidecar'])
def test_sidecar_format_dispatches_dedicated_parser(monkeypatch, format):
    class Document(Contract):
        schema_version: int
    seen = []
    def parse(artifacts, ref, limits, *, format, active):
        active()
        seen.append(format)
        return SimpleNamespace(document=Document(schema_version=1), outputs=(), bytes_written=0)
    monkeypatch.setattr('photo_worker.operations.interchange.parse.stage_sidecar_archive', parse)
    monkeypatch.setattr('photo_worker.operations.interchange.parse.stage_photoeditor_bundle',
        lambda *args: pytest.fail('sidecar treated as bundle'))
    value = params(format)
    quota = ArchiveLimits(10000, 10000, 10)
    result = execute_parse(context(value), Artifacts(value), ParseInstallation(Admission([quota, quota, quota])))
    assert seen == [format] and len(result.outputs) == 1


@pytest.mark.parametrize("changed", [False, True])
def test_success_declares_files_and_rechecks_authority(monkeypatch, changed):
    class Document(Contract):
        schema_version: int
    document = Document(schema_version=1)
    file = ArtifactRef(artifact_id=uuid4(), sha256="b" * 64)
    observed = []

    def parse(artifacts, ref, limits, active):
        observed.append(limits)
        return SimpleNamespace(document=document, outputs=(file,), bytes_written=5)

    monkeypatch.setattr('photo_worker.operations.interchange.parse.stage_photoeditor_bundle', parse)
    value = params()
    quota = ArchiveLimits(20000, 20000, 20)
    artifacts = Artifacts(value)
    installation = ParseInstallation(Admission([quota, ArchiveLimits(10000, 10000, 10) if changed else quota, quota]))
    if changed:
        with pytest.raises(OperationFailure, match="INTERCHANGE_SOURCE_STALE"):
            execute_parse(context(value), artifacts, installation)
        assert not artifacts.roots
    else:
        result = execute_parse(context(value), artifacts, installation)
        assert result.outputs[1:] == [file] and len(result.outputs) == 2
        assert result.bytes_written == 5 + len(canonical_bytes(document))
        assert result.review_required is False
    assert observed == [ArchiveLimits(20000, 10000, 10)]


@pytest.mark.parametrize('phase', [1, 2, 3])
@pytest.mark.parametrize('failure', ['mutate', 'cancel'])
def test_admission_boundaries_own_params_and_never_return_cancelled_success(monkeypatch, phase, failure):
    class Document(Contract):
        schema_version: int
    document = Document(schema_version=1)
    parses = []

    def parse(artifacts, ref, limits, active):
        parses.append(ref.model_copy(deep=True))
        return SimpleNamespace(document=document, outputs=(), bytes_written=0)

    monkeypatch.setattr('photo_worker.operations.interchange.parse.stage_photoeditor_bundle', parse)
    value = params()
    original = value.model_copy(deep=True)
    ctx = context(value)
    cancelled = False
    calls = 0
    quota = ArchiveLimits(20000, 20000, 20)

    def active():
        if cancelled:
            raise RuntimeError('fixture lease lost')

    def admit(context, candidate):
        nonlocal cancelled, calls
        calls += 1
        if calls == phase:
            if failure == 'mutate':
                candidate.request.bundle.sha256 = 'f' * 64
            else:
                cancelled = True
        return quota

    ctx.check_active = active
    artifacts = Artifacts(value)
    expected = OperationFailure if failure == 'mutate' else RuntimeError
    with pytest.raises(expected):
        execute_parse(ctx, artifacts, ParseInstallation(SimpleNamespace(admit=admit)))
    assert calls == phase
    assert value == original
    assert len(parses) == int(phase > 1)
    assert len(artifacts.roots) == int(phase == 3)
