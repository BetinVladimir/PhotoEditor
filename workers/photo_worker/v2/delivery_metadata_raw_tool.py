"""Bounded read-only ExifTool extraction for a caller-verified RAW original."""
import os
import hashlib
from pathlib import Path
import selectors
import stat
import subprocess
from time import monotonic

from .delivery_metadata_raw_extract import MAX_RAW_EXIF_JSON_BYTES, extract_raw_delivery_metadata

RAW_METADATA_FILETYPES = {
    'image/x-adobe-dng': 'DNG', 'image/x-canon-cr2': 'CR2',
    'image/x-nikon-nef': 'NEF', 'image/x-sony-arw': 'ARW',
    'image/x-fuji-raf': 'RAF', 'image/x-olympus-orf': 'ORF',
    'image/x-panasonic-rw2': 'RW2',
}


def verify_raw_tool(executable, expected_sha, check_active):
    if (type(executable) is not str or not os.path.isabs(executable)
            or type(expected_sha) is not str or len(expected_sha) != 64
            or any(c not in '0123456789abcdef' for c in expected_sha)
            or Path(executable).resolve(strict=True) != Path(executable)):
        raise ValueError('DELIVERY_RAW_METADATA_TOOL_PIN')
    descriptor = os.open(executable, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        before = os.fstat(descriptor)
        if (not stat.S_ISREG(before.st_mode) or before.st_nlink != 1
                or not 1 <= before.st_size <= 16 * 1024 * 1024
                or not os.access(executable, os.X_OK)):
            raise ValueError('DELIVERY_RAW_METADATA_TOOL_FILE')
        digest = hashlib.sha256()
        with os.fdopen(descriptor, 'rb', closefd=False) as stream:
            while True:
                check_active()
                block = stream.read(65536)
                if not block:
                    break
                digest.update(block)
        identity = lambda s: (s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns,
                              s.st_ctime_ns, s.st_mode, s.st_nlink)
        if (digest.hexdigest() != expected_sha or identity(before) != identity(os.fstat(descriptor))
                or identity(before) != identity(os.stat(executable, follow_symlinks=False))):
            raise ValueError('DELIVERY_RAW_METADATA_TOOL_CHANGED')
        return identity(before)
    finally:
        os.close(descriptor)


def read_raw_metadata(executable, path, mime, *, expected_tool_sha, include_gps=False,
                      check_active=lambda: None,
                      timeout=120, popen=subprocess.Popen, clock=monotonic):
    if (not isinstance(executable, str) or not os.path.isabs(executable)
            or not os.path.isfile(executable) or not os.access(executable, os.X_OK)
            or not os.path.isabs(os.fspath(path)) or mime not in RAW_METADATA_FILETYPES
            or type(timeout) not in (int, float)
            or not 0 < timeout <= 120 or type(include_gps) is not bool):
        raise ValueError('DELIVERY_RAW_METADATA_TOOL_INPUT')
    # Explicit tags prevent maker notes, embedded previews and all unapproved
    # metadata from entering the subprocess output. -G1 disambiguates groups.
    tags = ['-FileType', '-EXIF:Artist', '-EXIF:Copyright', '-EXIF:Make', '-EXIF:Model',
            '-EXIF:LensModel', '-EXIF:DateTimeOriginal']
    if include_gps:
        tags += ['-GPS:GPSLatitude', '-GPS:GPSLatitudeRef', '-GPS:GPSLongitude',
                 '-GPS:GPSLongitudeRef', '-GPS:GPSAltitude', '-GPS:GPSAltitudeRef']
    # Keep a complete scan: on some RAW layouts an early-stop mode can omit
    # otherwise allowlisted EXIF. Output and elapsed time are bounded below.
    args = [executable, '-j', '-G1', '-n', *tags, '--', os.fspath(path)]
    check_active()
    tool_identity = verify_raw_tool(executable, expected_tool_sha, check_active)
    process = popen(args, shell=False, stdin=subprocess.DEVNULL, stdout=subprocess.PIPE,
                    stderr=subprocess.DEVNULL, close_fds=True)
    output = bytearray()
    deadline = clock() + timeout
    try:
        with selectors.DefaultSelector() as selector:
            selector.register(process.stdout, selectors.EVENT_READ)
            while True:
                check_active()
                remaining = deadline - clock()
                if remaining <= 0:
                    raise subprocess.TimeoutExpired(args, timeout)
                if selector.select(min(0.25, remaining)):
                    block = os.read(process.stdout.fileno(),
                                    min(8192, MAX_RAW_EXIF_JSON_BYTES + 1 - len(output)))
                    if not block:
                        break
                    output.extend(block)
                    if len(output) > MAX_RAW_EXIF_JSON_BYTES:
                        raise ValueError('DELIVERY_RAW_METADATA_TOOL_OUTPUT')
            remaining = deadline - clock()
            if remaining <= 0:
                raise subprocess.TimeoutExpired(args, timeout)
            code = process.wait(timeout=remaining)
            check_active()
            if code != 0:
                raise ValueError('DELIVERY_RAW_METADATA_TOOL_FAILED')
            verify_raw_tool(executable, expected_tool_sha, check_active)
            after = os.stat(executable, follow_symlinks=False)
            if tool_identity != (after.st_dev, after.st_ino, after.st_size,
                                 after.st_mtime_ns, after.st_ctime_ns, after.st_mode, after.st_nlink):
                raise ValueError('DELIVERY_RAW_METADATA_TOOL_CHANGED')
            return extract_raw_delivery_metadata(bytes(output), include_gps=include_gps,
                                                 expected_filetype=RAW_METADATA_FILETYPES[mime])
    finally:
        if process.poll() is None:
            try:
                process.kill()
            except ProcessLookupError:
                pass
        process.wait()
        process.stdout.close()
