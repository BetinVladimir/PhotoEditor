"""Source IO stops at lease loss and unwinds all scoped inputs/maps."""
from contextlib import contextmanager
from types import SimpleNamespace as NS

import numpy as np
import pytest

from photo_worker.operations.learning import source
from photo_worker.runtime.executor import LeaseLost


STAGES = ['contract:image', 'contract:guard', 'contract:effective', 'contract:region']
for _name in ('rgb', 'alpha', 'guard_pixels', 'effective_pixels', 'region_pixels'):
    STAGES.extend(('input:' + _name, 'map:' + _name))


@pytest.mark.parametrize('stop_at', STAGES)
def test_holdout_source_cancellation_unwinds_exact_acquired_scope(monkeypatch, stop_at):
    events, acquired, released = [], [], []
    cancelled = False
    canvas = NS(width=2, height=2)
    image = NS(dtype='float32', color=NS(profile_id='work_rec2020_gamma24_v1',
        encoding='gamma24', icc_sha256='a' * 64), canvas=canvas,
        transform_chain=[], pixels='rgb', alpha='alpha')
    def mask(name, protected=False):
        return NS(protected=protected, canvas=canvas, transform_chain=[], artifact=name)
    values = dict(image=image, guard=mask('guard_pixels', True),
                  effective=mask('effective_pixels'), region=mask('region_pixels'))
    case = NS(binding=NS(source='image', canvas_hash='b' * 64), working_image='image',
              protected='guard', effective_mask='effective',
              region_masks=[NS(region_id='face', mask='region')])

    def record(event):
        nonlocal cancelled
        events.append(event)
        if event == stop_at:
            cancelled = True

    def active():
        if cancelled:
            raise LeaseLost('source lease lost')

    def contract(ref, model):
        record('contract:' + ref)
        return values[ref]

    @contextmanager
    def input_scope(ref, **kwargs):
        event = 'input:' + ref
        acquired.append(event)
        record(event)
        try:
            yield NS(path=ref)
        finally:
            released.append(event)

    @contextmanager
    def array_scope(path, shape, *args, **kwargs):
        event = 'map:' + path
        acquired.append(event)
        record(event)
        try:
            yield np.zeros(shape, np.float32)
        finally:
            released.append(event)

    monkeypatch.setattr(source, 'canonical_hash', lambda value: 'b' * 64)
    monkeypatch.setattr(source, 'read_array', array_scope)
    monkeypatch.setattr(source, 'read_alpha_array', array_scope)
    artifacts = NS(contract=contract, input=input_scope)
    with pytest.raises(LeaseLost, match='source lease lost'):
        with source.open_holdout_source(artifacts, case, 'a' * 64, active):
            pytest.fail('yielded cancelled source')
    assert events == STAGES[:STAGES.index(stop_at) + 1]
    assert released == list(reversed(acquired))
