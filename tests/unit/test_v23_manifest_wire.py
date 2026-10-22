"""The Go publisher's canonical manifest must satisfy the worker contract."""

from pathlib import Path
import sys

# Fixed selected-source helper; never a caller-supplied module or cached foreign copy.
_HELPER_PATH = Path(__file__).resolve().parents[1] / 'platform' / 'go_parity_owner.py'
sys.path.insert(0, str(_HELPER_PATH.parent))
import go_parity_owner as _go_owner
if Path(_go_owner.__file__).resolve() != _HELPER_PATH:
    raise ImportError('GO_PARITY_SELECTED_HELPER_ORIGIN')


from photo_worker.operations.learning.contracts import TrainingManifest


def test_go_training_manifest_matches_worker_contract():
    with _go_owner.go_parity_owner(__file__) as owner:
        output = owner.temporary / 'training-manifest.json'
        run = owner.run(
            ['go', 'test', './internal/v2learning',
             '-run', '^TestCompileTrainingManifestPinsSplitAndRenderCases$',
             '-count=1'], timeout=60,
            extra_env={'PHOTO_TEST_MANIFEST_OUTPUT': str(output)},
        )
        assert run.returncode == 0, run.stdout + run.stderr
        manifest = TrainingManifest.model_validate_json(output.read_bytes())
    assert len(manifest.examples) == 60
    assert len(manifest.consents) == 1
    assert len({item.session_id for item in manifest.examples
                if item.split == 'holdout'}) == 2
