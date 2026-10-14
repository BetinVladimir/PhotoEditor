import io
import os

import pytest

from photo_worker.operations.provenance.bounded_stream import BoundedSigningStream


def test_random_access_output_and_exact_ceiling():
    backing = io.BytesIO()
    with BoundedSigningStream(backing, 8) as stream:
        assert stream.write(b'abcdefgh') == 8
        assert stream.seek(-4, os.SEEK_END) == 4
        assert stream.write(b'WXYZ') == 4
        stream.seek(0)
        result = bytearray(8)
        assert stream.readinto(result) == 8
        assert result == b'abcdWXYZ'
        assert stream.read() == b''
    assert not backing.closed


@pytest.mark.parametrize('operation', ['write', 'seek_set', 'seek_cur',
                                      'seek_end', 'truncate', 'negative_seek'])
def test_refusal_does_not_change_output_or_position(operation):
    backing = io.BytesIO(b'abcd')
    stream = BoundedSigningStream(backing, 8)
    stream.seek(3)
    actions = {
        'write': lambda: stream.write(b'123456'),
        'seek_set': lambda: stream.seek(9),
        'seek_cur': lambda: stream.seek(6, os.SEEK_CUR),
        'seek_end': lambda: stream.seek(5, os.SEEK_END),
        'truncate': lambda: stream.truncate(9),
        'negative_seek': lambda: stream.seek(-1),
    }
    with pytest.raises(ValueError, match='SIGNER_OUTPUT_LIMIT'):
        actions[operation]()
    assert backing.getvalue() == b'abcd'
    assert backing.tell() == 3


def test_sparse_writes_and_truncation_are_bounded():
    backing = io.BytesIO()
    stream = BoundedSigningStream(backing, 8)
    stream.seek(7)
    stream.write(b'x')
    assert len(backing.getvalue()) == 8
    with pytest.raises(ValueError):
        stream.write(b'y')
    stream.truncate(2)
    assert len(backing.getvalue()) == 2


def test_no_descriptor_bypass_and_closed_wrapper_is_unusable():
    backing = io.BytesIO()
    stream = BoundedSigningStream(backing, 8)
    with pytest.raises(io.UnsupportedOperation):
        stream.fileno()
    stream.close()
    with pytest.raises(ValueError):
        stream.write(b'x')
    assert not backing.closed


@pytest.mark.parametrize('maximum', [True, 0, -1, (512 << 20) + 1])
def test_invalid_budget(maximum):
    with pytest.raises(ValueError):
        BoundedSigningStream(io.BytesIO(), maximum)


def test_existing_oversize_output_is_refused():
    with pytest.raises(ValueError):
        BoundedSigningStream(io.BytesIO(b'123456789'), 8)


@pytest.mark.parametrize('operation',[lambda stream:stream.write(b'x'),
    lambda stream:stream.read(1),lambda stream:stream.seek(0),
    lambda stream:stream.tell(),lambda stream:stream.truncate(0),
    lambda stream:stream.readinto(bytearray(1))])
def test_cancelled_sdk_callback_refuses_io_but_allows_cleanup(operation):
    live=True
    def active():
        if not live:raise RuntimeError('cancelled')
    backing=io.BytesIO(b'original')
    stream=BoundedSigningStream(backing,64,check_active=active)
    live=False
    with pytest.raises(RuntimeError,match='cancelled'):operation(stream)
    stream.close()
    assert backing.getvalue()==b'original' and not backing.closed


@pytest.mark.parametrize('operation',[lambda stream:stream.write(b'x'),
    lambda stream:stream.read(1),lambda stream:stream.seek(0),
    lambda stream:stream.tell(),lambda stream:stream.truncate(0),
    lambda stream:stream.readinto(bytearray(1))])
def test_boolean_denial_refuses_sdk_callback_without_changing_bytes(operation):
    live=True
    backing=io.BytesIO(b'original')
    stream=BoundedSigningStream(backing,64,check_active=lambda:live)
    live=False
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        operation(stream)
    stream.close()
    assert backing.getvalue()==b'original' and not backing.closed


def test_boolean_denial_refuses_stream_initialization_before_backing_io():
    class Backing(io.BytesIO):
        def tell(self):pytest.fail('backing IO after cancellation')
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        BoundedSigningStream(Backing(),64,check_active=lambda:False)


@pytest.mark.parametrize('operation', ['read', 'readinto', 'write', 'seek', 'tell', 'truncate'])
def test_cancellation_during_backing_call_never_returns_sdk_result(operation):
    live = True
    armed = False

    def active():
        if not live:
            raise RuntimeError('cancelled during backing IO')

    class CancellingBacking(io.BytesIO):
        def cancel(self, name):
            nonlocal live
            if armed and (name == operation or operation == 'readinto' and name == 'read'):
                live = False

        def read(self, size=-1):
            result = super().read(size)
            self.cancel('read')
            return result

        def write(self, data):
            result = super().write(data)
            self.cancel('write')
            return result

        def seek(self, offset, whence=0):
            result = super().seek(offset, whence)
            self.cancel('seek')
            return result

        def tell(self):
            result = super().tell()
            self.cancel('tell')
            return result

        def truncate(self, size=None):
            result = super().truncate(size)
            self.cancel('truncate')
            return result

    backing = CancellingBacking(b'original')
    stream = BoundedSigningStream(backing, 64, check_active=active)
    destination = bytearray(b'unchanged')
    actions = dict(read=lambda: stream.read(1), readinto=lambda: stream.readinto(destination),
                   write=lambda: stream.write(b'x'), seek=lambda: stream.seek(1),
                   tell=stream.tell, truncate=lambda: stream.truncate(1))
    armed = True
    try:
        with pytest.raises(RuntimeError, match='cancelled during backing IO'):
            actions[operation]()
        if operation == 'readinto':
            assert destination == b'unchanged'
    finally:
        stream.close()
    assert not backing.closed
