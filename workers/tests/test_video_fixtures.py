"""Video fixture generator must produce the documented frame counts (needs ffmpeg; a skip is not acceptance)."""

import importlib.util
import shutil
from pathlib import Path

import pytest

_GEN = Path(__file__).resolve().parents[2] / 'tests' / 'fixtures' / 'video' / 'make_fixtures.py'


def _load():
    spec = importlib.util.spec_from_file_location('make_fixtures', _GEN)
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def test_generator_exposes_required_functions():
    mod = _load()
    for name in ('make_hard_cuts', 'make_dissolve', 'make_fades', 'make_sine20', 'main'):
        assert callable(getattr(mod, name))
    assert mod.EXPECT_FRAMES['hard_cuts.mp4'] == 150


@pytest.mark.skipif(not (shutil.which('ffmpeg') and shutil.which('ffprobe')), reason='ffmpeg not found')
def test_generated_fixtures_match_frame_counts(tmp_path):
    mod = _load()
    with pytest.raises(SystemExit) as exc:
        mod.main(tmp_path)
    assert exc.value.code == 0
    assert mod.frames(tmp_path / 'hard_cuts.mp4') == 150
