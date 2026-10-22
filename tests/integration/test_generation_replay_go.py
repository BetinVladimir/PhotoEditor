"""Real Go generation replay/authority schedules in a private migrated DB.

Authored for the later deep-testing phase. No shared development data is used.
"""
import json
import os
from pathlib import Path
import subprocess
import psycopg
from psycopg.conninfo import make_conninfo
import pytest
from generation_replay_fixture import generation_replay_fixtures


def run_generation_suite(database, test, marker, *, authority_mode=None, authority_target=None):
    with psycopg.connect(**database) as seed:
        fixtures = generation_replay_fixtures(seed)
    environment = dict(os.environ, PHOTO_TEST_DATABASE_URL=make_conninfo(**database),
        PHOTO_TEST_DISPOSABLE='1', PHOTO_TEST_GENERATION_REPLAY=json.dumps(fixtures))
    if authority_mode is not None:
        environment['PHOTO_TEST_GENERATION_AUTHORITY_MODE'] = authority_mode
    if authority_target is not None:
        environment['PHOTO_TEST_GENERATION_AUTHORITY_TARGET'] = authority_target
    root = Path(__file__).resolve().parents[2]
    result = subprocess.run(['go', 'test', './internal/interactive', '-run', f'^{test}$',
        '-count=1', '-timeout=45s', '-v'], cwd=root/'backend', env=environment,
        capture_output=True, text=True, timeout=75)
    assert result.returncode == 0, result.stdout + result.stderr
    assert marker in result.stdout, result.stdout + result.stderr
    assert f'--- PASS: {test}' in result.stdout, result.stdout + result.stderr


def test_generation_replay_survives_mutable_admission_changes(database):
    run_generation_suite(database, 'TestIntegrationGenerationReplayAfterMutableAdmissionChanges',
        'GENERATION_REPLAY_MUTABLE_AUTHORITY_OK')


@pytest.mark.parametrize('target', ['replay', 'abandon'])
@pytest.mark.parametrize('mode', ['revoked', 'expired', 'tenant_disabled', 'role_downgraded',
    'project_archived', 'source_deleted'])
def test_generation_replay_rechecks_authority_after_actual_receipt_wait(database, mode, target):
    run_generation_suite(database, 'TestIntegrationGenerationReplayReauthorizesAfterWait',
        'GENERATION_REPLAY_AUTHORITY_WAIT_OK', authority_mode=mode, authority_target=target)
