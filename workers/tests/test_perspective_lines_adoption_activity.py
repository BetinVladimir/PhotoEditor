"""V29 detector adoption refuses revoked activity before object and SQL reads."""
from types import SimpleNamespace
from pathlib import Path
from uuid import uuid4

import pytest

from photo_worker.operations.optical.perspective_lines_installation import PerspectiveLinesInstallation
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.perspective_lines_adoption_prepare import prepare_perspective_lines_adoption
from photo_worker.v2.perspective_lines_adoption_evidence import load_perspective_lines_adoption_evidence
from photo_worker.v2.perspective_lines_replay import verify_pinned_perspective_lines_replay
from photo_worker.v2.perspective_lines_validation import verify_perspective_lines_result


def test_detector_prepare_false_activity_prevents_object_read():
    class Reader:
        def read_json(self, *_args):
            raise AssertionError('cancelled detector adoption read object')
    installation = PerspectiveLinesInstallation(None, 'a'*64, 'b'*64)
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        prepare_perspective_lines_adoption(object(), Reader(), uuid4(), uuid4(), object(), object(),
            installation=installation, scratch_root=object(), max_bytes=1, ram_max_bytes=1,
            check_active=lambda: False)


def test_detector_evidence_false_activity_prevents_sql():
    def forbidden(*_args):
        raise AssertionError('cancelled detector adoption reached SQL')
    connection = SimpleNamespace(autocommit=True, execute=forbidden)
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        load_perspective_lines_adoption_evidence(connection, uuid4(), uuid4(), object(), b'{}',
                                                  check_active=lambda: False)


def test_detector_replay_false_activity_prevents_source_read():
    class Reader:
        def read_json(self, *_args):
            raise AssertionError('cancelled detector replay read source')
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        verify_pinned_perspective_lines_replay(Reader(), object(), object(), expected_base=object(),
            working_icc_sha='a'*64, runtime_lock=object(), runtime_sha='b'*64,
            scratch_root=Path('/tmp'), max_bytes=1, ram_max_bytes=1,
            check_active=lambda: False)


def test_detector_result_false_activity_prevents_evidence_read():
    class Reader:
        def evidence(self, _ref):
            raise AssertionError('cancelled detector result read evidence')
    with pytest.raises(AdoptionInvalid, match='ADOPTION_INACTIVE'):
        verify_perspective_lines_result(Reader(), object(), object(), expected_base=object(),
                                        working_icc_sha='a'*64, check_active=lambda: False)
