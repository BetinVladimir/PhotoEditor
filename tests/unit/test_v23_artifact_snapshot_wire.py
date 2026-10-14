"""The Go training snapshot compiler emits the specified ArtifactSnapshot."""

import json
import os
from pathlib import Path
import subprocess

from jsonschema import Draft202012Validator, FormatChecker


def test_go_training_snapshot_matches_contract(tmp_path):
    root = Path(__file__).resolve().parents[2]
    output = tmp_path / 'training-snapshot.json'
    env = os.environ.copy()
    env['PHOTO_TEST_SNAPSHOT_OUTPUT'] = str(output)
    env['GOCACHE'] = str(root / 'work' / 'go-cache')
    run = subprocess.run(
        ['go', 'test', './internal/v2learning',
         '-run', '^TestCompileTrainingArtifactSnapshot$', '-count=1'],
        cwd=root / 'backend', env=env,
        capture_output=True, text=True, timeout=60,
    )
    assert run.returncode == 0, run.stdout + run.stderr
    schemas = json.loads((root / 'docs/implementation-v2/contracts/schemas.json').read_text())
    contract = {**schemas, '$ref': '#/$defs/ArtifactSnapshot'}
    Draft202012Validator(contract, format_checker=FormatChecker()).validate(
        json.loads(output.read_bytes()))


def test_holdout_render_case_schema_includes_current_optional_evidence():
    root = Path(__file__).resolve().parents[2]
    schemas = json.loads((root / 'docs/implementation-v2/contracts/schemas.json').read_text())
    properties = schemas['$defs']['RenderCase']['properties']
    assert properties['background_asset'] == {'$ref': '#/$defs/FrozenBackgroundAsset'}
    assert properties['defect_evidence'] == {'$ref': '#/$defs/ArtifactRef'}
    assert properties['dust_evidence'] == {'$ref': '#/$defs/ArtifactRef'}
