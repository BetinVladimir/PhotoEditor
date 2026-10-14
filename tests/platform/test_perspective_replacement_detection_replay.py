from uuid import uuid4
import pytest
from photo_worker.v2 import perspective_replacement_detection_replay as module
from photo_worker.v2.adoption import AdoptionInvalid


def test_replacement_replay_requires_authoritative_loader():
    with pytest.raises(ValueError, match='REPLAY_CONFIGURATION'):
        module.prepare_replacement_detection_replay(None,uuid4(),None,None,b'{}',
            installation=None,load_current_evidence=None,check_active=lambda:None)


@pytest.mark.parametrize('descriptor', [b'{}', b'[]', b'{ "schema_version":1}', b'x' * (3*1024*1024+1)])
def test_replacement_replay_refuses_foreign_descriptor_before_source_io(monkeypatch,descriptor):
    monkeypatch.setattr(module,'validate_perspective_lines_adoption_installation',lambda value:object())
    def forbidden():
        raise AssertionError('evidence IO must not occur')
    with pytest.raises(AdoptionInvalid,match='DESCRIPTOR_INVALID'):
        module.prepare_replacement_detection_replay(None,uuid4(),None,None,descriptor,
            installation=None,load_current_evidence=forbidden,check_active=lambda:None)


def test_replacement_replay_observes_cancellation_before_installation():
    class Cancelled(Exception): pass
    def cancel(): raise Cancelled()
    with pytest.raises(Cancelled):
        module.prepare_replacement_detection_replay(None,uuid4(),None,None,b'{}',
            installation=None,load_current_evidence=lambda:None,check_active=cancel)
