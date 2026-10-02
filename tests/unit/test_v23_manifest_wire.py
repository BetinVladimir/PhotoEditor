"""The Go publisher's canonical manifest must satisfy the worker contract."""

import os
from pathlib import Path
import subprocess

from photo_worker.operations.learning.contracts import TrainingManifest


def test_go_training_manifest_matches_worker_contract(tmp_path):
    root = Path(__file__).resolve().parents[2]
    output = tmp_path / 'training-manifest.json'
    env = os.environ.copy()
    env['PHOTO_TEST_MANIFEST_OUTPUT'] = str(output)
    env['GOCACHE'] = str(root / 'work' / 'go-cache')
    run = subprocess.run(
        ['go', 'test', './internal/v2learning',
         '-run', '^TestCompileTrainingManifestPinsSplitAndRenderCases$',
         '-count=1'], cwd=root / 'backend', env=env,
        capture_output=True, text=True, timeout=60,
    )
    assert run.returncode == 0, run.stdout + run.stderr
    manifest = TrainingManifest.model_validate_json(output.read_bytes())
    assert len(manifest.examples) == 60
    assert len(manifest.consents) == 1
    assert len({item.session_id for item in manifest.examples
                if item.split == 'holdout'}) == 2
