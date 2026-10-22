"""Actual media encoder output metadata consumed by the Render client loader.

Catalog/signing transport is controlled; this is not PostgreSQL/S3 acceptance.
"""
import json
import os
from pathlib import Path
import subprocess
import tempfile
import pytest
from test_render_result_preview import test_render_image_result_through_real_media_encoder as exercise_encoder



def assert_live_vitest_report(path, expected, record_property):
    """Require executed child cases; an exit-zero skipped suite is insufficient."""
    report = json.loads(path.read_text())
    assert report['success'] is True, report
    assert report['numPassedTests'] == expected, report
    assert report['numFailedTests'] == report['numPendingTests'] == report.get('numTodoTests', 0) == 0, report
    cases = [case for suite in report['testResults'] for case in suite['assertionResults']]
    assert len(cases) == expected and all(case['status'] == 'passed' for case in cases), cases
    record_property(path.stem, json.dumps({'passed': expected, 'skipped': 0,
        'cases': [case['fullName'] for case in cases]}, sort_keys=True))

def test_render_encoder_outputs_are_accepted_by_client(record_property):
    repo=Path(__file__).resolve().parents[2]
    runtime=repo/'.runtime';runtime.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='render-client-',dir=runtime) as directory:
        root=Path(directory);cases=[]
        for alpha in (False,True):
            folder=root/str(alpha);folder.mkdir()
            with pytest.MonkeyPatch.context() as patch:
                exercise_encoder(folder,alpha,patch,client_cases=cases)
        path=root/'cases.json';path.write_text(json.dumps(cases))
        env=dict(os.environ,PHOTO_RENDER_CLIENT_CASES=str(path))
        report_path = root / 'test_render_preview_client_contract-vitest.json'
        result=subprocess.run(['npm','test','--','--run','src/render-preview-worker.test.ts', '--maxWorkers=2','--reporter=json', '--outputFile='+str(report_path)],
            cwd=repo/'apps/photographer',env=env,capture_output=True,text=True,timeout=60)
        assert result.returncode==0,result.stdout+result.stderr+(report_path.read_text() if report_path.exists() else "")
        assert_live_vitest_report(report_path, 2, record_property)
