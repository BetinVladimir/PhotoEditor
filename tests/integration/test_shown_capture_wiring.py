"""Pure harness regressions; these are not physical shown-preview evidence."""
import ast
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]


def test_shown_probe_uses_distinct_production_installers_and_private_login():
    path=ROOT/'tests/integration/v27_shown_preview_wire_probe.py'
    source=path.read_text()
    ast.parse(source)
    assert 'sql_exposure_preview_factory(**options,presentation=' in source
    assert 'sql_shown_exposure_factory(**options)' in source
    assert 'execute_preview_task' in source and 'execute_shown_exposure_task' in source
    assert 'TaskDatabase(runtime_dsn)' in source
    assert 'run_worker_probe(main,' in source
    assert 'coordinator.adopt(command.tenant_id,task) is True' in source
    assert 'coordinator.adopt(command.tenant_id,task) is False' in source


def test_legacy_output_capture_cannot_supply_shown_ui_payload():
    source=(ROOT/'tests/integration/test_v27_semantic_qualification_objectstore.py').read_text()
    ast.parse(source)
    assert "with_alpha=='bootstrap_shown'" in source
    assert 'output_ui=subprocess.run' not in source
    lifecycle=(ROOT/'tests/integration/v27_shown_capture_lifecycle.py').read_text()
    ast.parse(lifecycle)
    assert 'PHOTO_TEST_V27_APPLIED_PREVIEW_RESPONSE_FILE=str(capture)' in lifecycle
    assert "assert_live_vitest_report(report,1,record_property)" in lifecycle
    assert 'TestIntegrationShownAppliedPreviewCapture' in lifecycle


def test_legacy_exposure_worker_inputs_never_use_admin_runtime():
    source=(ROOT/'tests/integration/v27_exposure_inputs_probe.py').read_text()
    ast.parse(source)
    assert 'with worker_login(make_conninfo(**database), task, pending_dispatch=True) as runtime_dsn:' in source
    assert 'TaskDatabase(make_conninfo(**database))' not in source
    assert source.count('TaskDatabase(runtime_dsn)')==2
