"""Run the V23 Go profile service against a disposable migrated database."""

import os
import subprocess

from psycopg.conninfo import make_conninfo

from conftest import ROOT


def test_v23_profile_service(database):
    env = os.environ.copy()
    env['PHOTO_TEST_DATABASE_URL'] = make_conninfo(**database)
    env['GOCACHE'] = str(ROOT / 'work' / 'go-cache')
    result = subprocess.run(
        ['go', 'test', './internal/v2learning',
         '-run', '^TestIntegrationProfileCreateGetAndListVersions$', '-count=1'],
        cwd=ROOT / 'backend', env=env, capture_output=True, text=True,
        timeout=90,
    )
    assert result.returncode == 0, result.stdout + result.stderr
