"""The Go training snapshot compiler emits the specified ArtifactSnapshot."""

import json
from pathlib import Path
import sys

# Fixed selected-source helper; never a caller-supplied module or cached foreign copy.
_HELPER_PATH = Path(__file__).resolve().parents[1] / 'platform' / 'go_parity_owner.py'
sys.path.insert(0, str(_HELPER_PATH.parent))
import go_parity_owner as _go_owner
if Path(_go_owner.__file__).resolve() != _HELPER_PATH:
    raise ImportError('GO_PARITY_SELECTED_HELPER_ORIGIN')


from jsonschema import Draft202012Validator, FormatChecker


def test_go_training_snapshot_matches_contract():
    root = Path(__file__).resolve().parents[2]
    with _go_owner.go_parity_owner(__file__) as owner:
        output = owner.temporary / 'training-snapshot.json'
        run = owner.run(
            ['go', 'test', './internal/v2learning',
             '-run', '^TestCompileTrainingArtifactSnapshot$', '-count=1'],
            timeout=60, extra_env={'PHOTO_TEST_SNAPSHOT_OUTPUT': str(output)},
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
