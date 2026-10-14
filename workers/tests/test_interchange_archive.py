from contextlib import contextmanager
from hashlib import sha256
from io import BytesIO
import stat
import zipfile

import pytest

from photo_worker.v2.interchange_archive import (
    ArchiveLimits, InvalidInterchangeArchive, interchange_path, verify_archive,
)


def bundle(entries, compression=zipfile.ZIP_STORED):
    stream = BytesIO()
    with zipfile.ZipFile(stream, "w", compression=compression) as archive:
        for name, value in entries:
            archive.writestr(name, value)
    stream.seek(0)
    return stream


LIMITS = ArchiveLimits(1 << 20, 1 << 20, 30)


@pytest.mark.parametrize("path", ["../x", "/x", "x//y", "x/./y", "x/../y", "x\\y", "C:x", "x\0y", "x\ny", "x\ry", "x\ty", "x\x7fy", "e\u0301.jpg", "x/", "x" * 241])
def test_paths_refuse_unsafe_or_noncanonical_names(path):
    with pytest.raises(InvalidInterchangeArchive):
        interchange_path(path)


def test_inventory_hashes_actual_bytes_and_sorts_paths():
    stream = bundle([("z.jpg", b"image"), ("a.json", b"{}")])
    staged = {}

    @contextmanager
    def stage(path):
        output = BytesIO()
        yield output
        staged[path] = output.getvalue()

    result = verify_archive(stream, LIMITS, stage=stage)
    assert [item.path for item in result] == ["a.json", "z.jpg"]
    assert result[1].sha256 == sha256(b"image").hexdigest()
    assert result[1].bytes == 5
    assert staged == {"a.json": b"{}", "z.jpg": b"image"}
    assert not stream.closed


@pytest.mark.parametrize("names", [["x.jpg", "X.JPG"], ["x", "x/y/../z"], ["x", "bad\nname"]])
def test_collisions_and_traversal_are_rejected(names):
    with pytest.raises(InvalidInterchangeArchive):
        verify_archive(bundle([(name, b"x") for name in names]), LIMITS)


def test_symlinks_are_not_extracted():
    info = zipfile.ZipInfo("link")
    info.create_system = 3
    info.external_attr = (stat.S_IFLNK | 0o777) << 16
    with pytest.raises(InvalidInterchangeArchive):
        verify_archive(bundle([(info, b"../../secret")]), LIMITS)


def test_expansion_ratio_and_reservations_are_enforced():
    with pytest.raises(InvalidInterchangeArchive):
        verify_archive(bundle([("bomb", b"a" * 100000)], zipfile.ZIP_DEFLATED), LIMITS)
    for limits in [ArchiveLimits(10, 1000, 30), ArchiveLimits(10000, 1, 30), ArchiveLimits(10000, 1000, 1)]:
        with pytest.raises(InvalidInterchangeArchive):
            verify_archive(bundle([("a", b"aa"), ("b", b"bb")]), limits)


def test_crc_corruption_and_cancellation_do_not_return_inventory():
    source = bundle([("image", b"unique-content")])
    data = source.getvalue().replace(b"unique-content", b"broken-content")
    with pytest.raises(InvalidInterchangeArchive):
        verify_archive(BytesIO(data), LIMITS)
    with pytest.raises(InterruptedError):
        verify_archive(bundle([("image", b"bytes")]), LIMITS, cancelled=lambda: True)


def test_short_staging_write_is_failure():
    class ShortWriter:
        def write(self, value):
            return len(value) - 1

    @contextmanager
    def stage(_path):
        yield ShortWriter()

    with pytest.raises(OSError, match="short"):
        verify_archive(bundle([("image", b"bytes")]), LIMITS, stage=stage)
