"""Legacy inherited fields remain typed, hash checked and scratch bounded."""
from hashlib import sha256
from io import BytesIO
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.v2.adoption import ArtifactRef, ArtifactEvidence, AdoptionInvalid
from photo_worker.v2.array_evidence import ArrayEvidenceSession


@pytest.mark.parametrize('failure', [None, 'shape', 'dtype', 'nonfinite', 'hash', 'budget', 'output'])
def test_inherited_field_content_gate(tmp_path, failure):
    values = np.zeros((4, 5, 2), dtype=np.float32)
    if failure == 'shape': values = values[:, :, :1]
    if failure == 'dtype': values = values.astype(np.float64)
    if failure == 'nonfinite': values[0, 0, 0] = np.nan
    stream = BytesIO(); np.save(stream, values, allow_pickle=False)
    raw = stream.getvalue()
    ref = ArtifactRef(uuid4(), 'f'*64 if failure == 'hash' else sha256(raw).hexdigest())
    source = 'output' if failure == 'output' else 'input'
    class Reader:
        def evidence(self, value):
            assert value == ref
            return ArtifactEvidence(ref, 'application/octet-stream', len(raw), None, source)
        def iter_bytes(self, value, size):
            assert value == ref and size == len(raw)
            yield raw
    def verify():
        with ArrayEvidenceSession(Reader(), tmp_path, len(raw)-1 if failure == 'budget' else 4096) as arrays:
            field = arrays.array(ref, (4, 5, 2), source=source,
                legacy_input_npy=source == 'input', inverse_field=True)
            assert field.shape == (4, 5, 2) and field.dtype == np.float32
    if failure:
        with pytest.raises((AdoptionInvalid, ValueError)): verify()
    else: verify()
    assert not list(tmp_path.glob('adoption-*'))
