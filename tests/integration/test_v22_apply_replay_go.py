"""Exercise the API-role Go replay path against all deployed migrations."""

import os
import subprocess

from psycopg.conninfo import make_conninfo

from conftest import ROOT


def test_v22_analysis_apply_sealed_replay(database):
    env = os.environ.copy()
    env['PHOTO_TEST_DATABASE_URL'] = make_conninfo(**database)
    env['GOCACHE'] = str(ROOT / '.cache' / 'go')
    result = subprocess.run(
        ['go', 'test', './internal/v2sessions',
         '-run', '^TestIntegrationAnalysisApply(New|Replay)$', '-count=1'],
        cwd=ROOT / 'backend', env=env, capture_output=True, text=True,
        timeout=120,
    )
    assert result.returncode == 0, result.stdout + result.stderr
