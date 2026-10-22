"""Run the actual Go startup ACL gate in conftest's disposable migrated DB.

No production DSN fixture is imported. ACL changes stay inside a rolled-back
transaction; an exit-zero skipped Go test is explicitly not accepted.
"""
import os
from pathlib import Path
import subprocess

from psycopg.conninfo import make_conninfo


def test_retouch_startup_acl_gate_real_roles(database):
    repo = Path(__file__).resolve().parents[2]
    environment = dict(os.environ,
        PHOTO_TEST_DATABASE_URL=make_conninfo(**database),
        PHOTO_TEST_DISPOSABLE='1')
    result = subprocess.run(
        ['go', 'test', './internal/semanticreview', '-run',
         '^TestIntegrationShownRetouchStartupACL$', '-count=1', '-timeout=60s', '-v'],
        cwd=repo / 'backend', env=environment, capture_output=True,
        text=True, timeout=90)
    assert result.returncode == 0, result.stdout + result.stderr
    assert 'V27_RETOUCH_STARTUP_ACL_OK' in result.stdout
    assert '--- PASS: TestIntegrationShownRetouchStartupACL' in result.stdout
