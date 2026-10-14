"""Seekable SDK destination with a hard logical file-size ceiling.

The wrapper deliberately does not expose fileno or an underlying stream: the
SDK must use its Python stream callbacks, including for random-access writes.
This bounds output disk use, not native SDK memory or execution time.
"""
import io
import operator
import os


class BoundedSigningStream(io.IOBase):
    def __init__(self, stream, maximum, *, check_active=lambda: None):
        self._stream = stream
        super().__init__()
        if type(maximum) is not int or not 1 <= maximum <= 512 << 20:
            raise ValueError('SIGNER_OUTPUT_LIMIT')
        if not callable(check_active):
            raise ValueError('SIGNER_STREAM_CANCELLATION')
        def active():
            if check_active() is False:
                raise RuntimeError('SIGNER_TASK_INACTIVE')
        self._active = active
        self._active()
        self._maximum = maximum
        position = stream.tell()
        end = stream.seek(0, os.SEEK_END)
        stream.seek(position)
        self._active()
        if not 0 <= position <= maximum or not 0 <= end <= maximum:
            raise ValueError('SIGNER_OUTPUT_LIMIT')

    def readable(self):
        return not self.closed and self._stream.readable()

    def writable(self):
        return not self.closed and self._stream.writable()

    def seekable(self):
        return not self.closed and self._stream.seekable()

    def tell(self):
        self._checkClosed()
        self._active()
        result = self._stream.tell()
        self._active()
        return result

    def seek(self, offset, whence=os.SEEK_SET):
        self._checkClosed()
        self._active()
        offset = operator.index(offset)
        position = self._stream.tell()
        if whence == os.SEEK_SET:
            target = offset
        elif whence == os.SEEK_CUR:
            target = position + offset
        elif whence == os.SEEK_END:
            end = self._stream.seek(0, os.SEEK_END)
            self._stream.seek(position)
            target = end + offset
        else:
            raise ValueError('SIGNER_OUTPUT_SEEK')
        if not 0 <= target <= self._maximum:
            raise ValueError('SIGNER_OUTPUT_LIMIT')
        result = self._stream.seek(target)
        self._active()
        return result

    def write(self, data):
        self._checkClosed()
        self._active()
        view = memoryview(data)
        if self.tell() + view.nbytes > self._maximum:
            raise ValueError('SIGNER_OUTPUT_LIMIT')
        result = self._stream.write(view)
        self._active()
        return result

    def truncate(self, size=None):
        self._checkClosed()
        self._active()
        size = self.tell() if size is None else operator.index(size)
        if not 0 <= size <= self._maximum:
            raise ValueError('SIGNER_OUTPUT_LIMIT')
        result = self._stream.truncate(size)
        self._active()
        return result

    def read(self, size=-1):
        self._checkClosed()
        self._active()
        size = operator.index(size)
        remaining = self._maximum - self.tell()
        result = self._stream.read(remaining if size < 0 else min(size, remaining))
        self._active()
        return result

    def readinto(self, buffer):
        data = self.read(memoryview(buffer).nbytes)
        memoryview(buffer).cast('B')[:len(data)] = data
        return len(data)

    def flush(self):
        self._checkClosed()
        self._stream.flush()
