"""Every acquired holdout output is released even after a flush failure."""
from contextlib import contextmanager
from types import SimpleNamespace

import numpy as np
import pytest

from photo_worker.operations.learning import workspace
from photo_worker.runtime.executor import LeaseLost


@pytest.mark.parametrize('failure', ['none', 'flush', 'allocation', 'cancel'])
def test_workspace_releases_all_acquired_outputs(monkeypatch, tmp_path, failure):
    acquired, closed = [], []
    released = []

    class Buffer:
        def __init__(self, index):
            self.index = index
            self._mmap = SimpleNamespace(close=lambda: closed.append(index))

        def __setitem__(self, key, value):
            pass

        def flush(self):
            if failure == 'flush' and self.index == 3:
                raise OSError('flush failed')

    def allocate(*args, **kwargs):
        if failure == 'allocation' and len(acquired) == 2:
            raise OSError('allocation failed')
        result = Buffer(len(acquired))
        acquired.append(result)
        return result

    @contextmanager
    def source(*args):
        yield SimpleNamespace(rgb=np.zeros((2, 2, 3), np.float32), alpha=None)

    @contextmanager
    def reserve(size):
        assert size == 4 * 40 + 16384
        try:
            yield SimpleNamespace(path=lambda name, suffix: tmp_path / (name + suffix),
                                  validate=lambda: None)
        finally:
            released.append(True)

    def active():
        if failure == 'cancel' and len(acquired) == 2:
            raise LeaseLost('lost during workspace allocation')

    monkeypatch.setattr(workspace, 'open_holdout_source', source)
    monkeypatch.setattr(workspace.np.lib.format, 'open_memmap', allocate)
    artifacts = SimpleNamespace(scratch=SimpleNamespace(reserve=reserve))
    def run():
        with workspace.open_holdout_workspace(artifacts, None, 'a' * 64, active):
            assert len(acquired) == 4
    if failure == 'none':
        run()
    else:
        with pytest.raises(LeaseLost if failure == 'cancel' else OSError):
            run()
    assert len(acquired) == (2 if failure in ('allocation', 'cancel') else 4)
    assert closed == list(reversed(range(len(acquired))))
    assert released == [True]
