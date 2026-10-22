"""Pure report-gate tests; these are not live response/SDK evidence."""
import json
import pytest
from test_v27_semantic_qualification_objectstore import assert_live_vitest_report


@pytest.mark.parametrize('mutation', ('none', 'skipped', 'failed', 'missing_case', 'wrong_status', 'extra_case'))
def test_live_capture_gate_cannot_accept_successful_skipped_process(tmp_path, mutation):
    case = dict(status='passed', fullName='actual captured response consumer')
    report = dict(success=True, numPassedTests=1, numFailedTests=0, numPendingTests=0,
                  numTodoTests=0, testResults=[dict(assertionResults=[case])])
    if mutation == 'skipped':
        report.update(numPassedTests=0, numPendingTests=1)
        case['status'] = 'pending'
    elif mutation == 'failed':
        report.update(success=False, numPassedTests=0, numFailedTests=1)
        case['status'] = 'failed'
    elif mutation == 'missing_case':
        report['testResults'] = []
    elif mutation == 'wrong_status':
        case['status'] = 'pending'
    elif mutation == 'extra_case':
        report['testResults'][0]['assertionResults'].append(dict(case))
    path = tmp_path/'report.json'
    path.write_text(json.dumps(report))
    recorded = []
    call = lambda: assert_live_vitest_report(path, 1, lambda *args: recorded.append(args))
    if mutation == 'none':
        call()
        assert len(recorded) == 1 and json.loads(recorded[0][1])['skipped'] == 0
    else:
        with pytest.raises(AssertionError):
            call()
        assert recorded == []
