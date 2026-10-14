import pytest
from photo_worker.operations.interchange.import_admission import SQLImportAdmission
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2.interchange_plan import InterchangeImportParams
from test_interchange_import_adapter import adapter_fixture


@pytest.mark.parametrize('change', ['none', 'missing', 'extra', 'uuid', 'snapshot', 'bool_budget', 'zero_output'])
def test_sql_import_admission_requires_exact_bounded_authority(change):
    ctx, artifacts, initial, _, docs = adapter_fixture()
    params = artifacts.params(InterchangeImportParams);expected = initial.admit(ctx, params)
    result = dict(workflow_id=str(expected.workflow_id), project_id=str(expected.project_id),
        snapshot_digest=expected.snapshot_digest, max_input_json_bytes=expected.max_input_json_bytes,
        max_output_bytes=expected.max_output_bytes)
    if change == 'missing':result = None
    elif change == 'extra':result['unexpected'] = True
    elif change == 'uuid':result['workflow_id'] = 'invalid'
    elif change == 'snapshot':result['snapshot_digest'] = 'z'*64
    elif change == 'bool_budget':result['max_input_json_bytes'] = True
    elif change == 'zero_output':result['max_output_bytes'] = 0
    class Database:
        calls = 0
        def interchange_import_context(self, lease, raw):
            self.calls += 1
            assert lease is ctx.lease and raw == docs.params_jcs
            return result
    database = Database();admission = SQLImportAdmission(database)
    if change == 'none':
        assert admission.admit(ctx, params) == expected
    else:
        with pytest.raises(OperationFailure, match='INTERCHANGE_SOURCE_STALE'):
            admission.admit(ctx, params)
    assert database.calls == 1


def test_sql_import_admission_rejects_modified_params_before_database():
    ctx, artifacts, _, _, _ = adapter_fixture();params = artifacts.params(InterchangeImportParams)
    class Database:
        def interchange_import_context(self, *args):
            pytest.fail('invalid Params reached SQL')
    params.evidence = None
    with pytest.raises(OperationFailure, match='INTERCHANGE_SOURCE_STALE'):
        SQLImportAdmission(Database()).admit(ctx, params)
