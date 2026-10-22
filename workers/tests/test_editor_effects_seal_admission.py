"""Authoring only: seal admission never substitutes a deployment callback."""
from hashlib import sha256
from types import SimpleNamespace
import pytest
from photo_worker.contracts.base import Contract, canonical_bytes
from photo_worker.operations.editor.seal_admission import SQLEditorEffectsSealAdmission
from photo_worker.runtime.executor import OperationFailure


class Params(Contract):
    schema_version: int = 1


class Context:
    def __init__(self, *, backend='cpu', operation='editor.effects_seal', digest=None):
        self.checks = 0
        self.lease = SimpleNamespace(execution_profile={'backend': backend}, command=SimpleNamespace(
            operation=operation, operation_version='1', resource_class='cpu',
            params=SimpleNamespace(sha256=digest or sha256(canonical_bytes(Params())).hexdigest())))
    def check_active(self):
        self.checks += 1


@pytest.mark.parametrize('answer', [False, None, 1, 'true'])
def test_seal_requires_literal_live_sql_acceptance(answer):
    database = SimpleNamespace(admit_editor_effects_seal=lambda lease, raw: answer)
    with pytest.raises(OperationFailure):
        SQLEditorEffectsSealAdmission(database).admit(Context(), Params())


def test_seal_rechecks_cancellation_after_live_authority():
    context = Context()
    calls = []
    database = SimpleNamespace(admit_editor_effects_seal=lambda lease, raw: calls.append(raw) or True)
    SQLEditorEffectsSealAdmission(database).admit(context, Params())
    assert calls == [canonical_bytes(Params())]
    assert context.checks == 2


@pytest.mark.parametrize('options', [{'backend': 'mps'}, {'operation': 'render.compose'}, {'digest': '0'*64}])
def test_seal_rejects_foreign_task_before_sql(options):
    calls = []
    database = SimpleNamespace(admit_editor_effects_seal=lambda *args: calls.append(args) or True)
    with pytest.raises(OperationFailure):
        SQLEditorEffectsSealAdmission(database).admit(Context(**options), Params())
    assert calls == []
