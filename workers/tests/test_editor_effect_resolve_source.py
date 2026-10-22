from types import SimpleNamespace
import pytest
from photo_worker.operations.style.resolve import validate_override_source

@pytest.mark.parametrize('sha', ['a'*64, 'b'*64])
def test_override_matches_only_current_envelope_or_encoded_source(sha):
    validate_override_source(SimpleNamespace(input_sha256=sha),
        SimpleNamespace(sha256='a'*64),
        SimpleNamespace(canvas=SimpleNamespace(source_sha256='b'*64)))

def test_override_cannot_rebind_to_foreign_capture_or_ancestor():
    with pytest.raises(ValueError, match='OVERRIDE_SOURCE_MISMATCH'):
        validate_override_source(SimpleNamespace(input_sha256='c'*64),
            SimpleNamespace(sha256='a'*64),
            SimpleNamespace(canvas=SimpleNamespace(source_sha256='b'*64)))

def test_job_override_has_no_per_image_binding():
    validate_override_source(SimpleNamespace(modules={}),
        SimpleNamespace(sha256='a'*64),
        SimpleNamespace(canvas=SimpleNamespace(source_sha256='b'*64)))
