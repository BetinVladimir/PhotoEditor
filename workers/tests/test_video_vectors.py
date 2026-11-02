import pytest

from video_vectors import load_vector


def test_time_vector_25fps_ticks():
    assert load_vector('time')['frame_ticks']['25/1'] == 28224000


def test_rejects_path_like_names():
    with pytest.raises(ValueError):
        load_vector('../x')
