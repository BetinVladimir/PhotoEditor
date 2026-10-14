import json
from hashlib import sha256
import sys

import pytest

from photo_worker.v2.delivery_metadata_raw_tool import read_raw_metadata


def tool(tmp_path, body):
    script = tmp_path / 'exiftool'
    script.write_text('#!' + sys.executable + '\n' + body)
    script.chmod(0o700)
    original = tmp_path / 'source.dng'
    original.write_bytes(b'fixture')
    return str(script), original


def test_bounded_tool_uses_explicit_exif_tags_and_gps_opt_in(tmp_path):
    payload = json.dumps([{'File:FileType': 'DNG', 'IFD0:Artist': 'A', 'GPS:GPSLatitude': 1,
                           'GPS:GPSLatitudeRef': 'N', 'GPS:GPSLongitude': 2,
                           'GPS:GPSLongitudeRef': 'E'}])
    executable, source = tool(tmp_path, 'import sys\n'
        'assert "-fast2" not in sys.argv and "-j" in sys.argv and "-G1" in sys.argv and "-n" in sys.argv\n'
        'assert "-GPS:GPSLatitude" in sys.argv\n'
        f'sys.stdout.write({payload!r})\n')
    pin=sha256(open(executable,'rb').read()).hexdigest()
    result = read_raw_metadata(executable, source, 'image/x-adobe-dng', expected_tool_sha=pin, include_gps=True)
    assert result.author == 'A' and result.gps.latitude == 1


def test_tool_refuses_oversized_stdout(tmp_path):
    executable, source = tool(tmp_path, 'import sys\n'
        'sys.stdout.write("x" * 70000)\n')
    with pytest.raises(ValueError, match='TOOL_OUTPUT'):
        read_raw_metadata(executable, source, 'image/x-adobe-dng', expected_tool_sha=sha256(open(executable,'rb').read()).hexdigest())


def test_tool_refuses_nonzero_exit(tmp_path):
    executable, source = tool(tmp_path, 'raise SystemExit(4)\n')
    with pytest.raises(ValueError, match='TOOL_FAILED'):
        read_raw_metadata(executable, source, 'image/x-adobe-dng', expected_tool_sha=sha256(open(executable,'rb').read()).hexdigest())


def test_tool_refuses_different_physical_format(tmp_path):
    executable, source = tool(tmp_path,
        'import sys\nsys.stdout.write(\'[{"File:FileType":"NEF"}]\')\n')
    with pytest.raises(ValueError, match='FILETYPE'):
        read_raw_metadata(executable, source, 'image/x-adobe-dng', expected_tool_sha=sha256(open(executable,'rb').read()).hexdigest())


def test_tool_refuses_changed_binary_before_execution(tmp_path):
    executable, source = tool(tmp_path, 'raise SystemExit(0)\n')
    with pytest.raises(ValueError, match='TOOL_CHANGED'):
        read_raw_metadata(executable, source, 'image/x-adobe-dng', expected_tool_sha='a'*64)
