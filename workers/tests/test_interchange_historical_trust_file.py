"""Real local trust-file IO refuses special files and changes during reading."""
from hashlib import sha256
import os
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.v2.interchange_historical_origin import load_historical_origin_trust


def fixture(tmp_path):
    issuer = uuid4()
    raw = canonical_bytes({'schema': 'historical-origin-trust/v1', 'issuers': [
        {'issuer_id': str(issuer), 'key_id': 'local', 'public_key_hex': (b'x' * 32).hex()}]})
    path = tmp_path / 'trust.json'
    path.write_bytes(raw)
    return path, raw, issuer


def test_trust_reads_pinned_regular_file(tmp_path):
    path, raw, issuer = fixture(tmp_path)
    authority = load_historical_origin_trust(path, sha256(raw).hexdigest())
    assert authority.trusted == {(issuer, 'local'): b'x' * 32}


@pytest.mark.parametrize('kind', ['symlink', 'fifo', 'directory', 'hardlink'])
def test_non_regular_or_shared_trust_file_is_refused(tmp_path, kind):
    path, raw, _ = fixture(tmp_path)
    other = tmp_path / 'other'
    if kind == 'symlink':
        path.rename(other)
        path.symlink_to(other)
    elif kind == 'fifo':
        path.unlink()
        os.mkfifo(path)
    elif kind == 'directory':
        path.unlink()
        path.mkdir()
    else:
        os.link(path, other)
    with pytest.raises(ValueError, match='TRUST_FILE'):
        load_historical_origin_trust(path, sha256(raw).hexdigest())


def test_same_bytes_new_inode_during_read_is_refused(tmp_path, monkeypatch):
    path, raw, _ = fixture(tmp_path)
    replacement = tmp_path / 'replacement'
    replacement.write_bytes(raw)
    read = os.read
    replaced = False

    def racing_read(descriptor, size):
        nonlocal replaced
        block = read(descriptor, size)
        if not replaced:
            replaced = True
            replacement.replace(path)
        return block

    monkeypatch.setattr(os, 'read', racing_read)
    with pytest.raises(ValueError, match='TRUST_FILE_CHANGED'):
        load_historical_origin_trust(path, sha256(raw).hexdigest())
    assert replaced


def test_parent_symlink_is_not_followed(tmp_path):
    directory = tmp_path / 'real'
    directory.mkdir()
    path, raw, _ = fixture(directory)
    link = tmp_path / 'link'
    link.symlink_to(directory, target_is_directory=True)
    with pytest.raises(ValueError, match='TRUST_FILE'):
        load_historical_origin_trust(link / path.name, sha256(raw).hexdigest())


def test_wrong_hash_refuses_unchanged_regular_file(tmp_path):
    path, _, _ = fixture(tmp_path)
    with pytest.raises(ValueError, match='TRUST_PIN'):
        load_historical_origin_trust(path, '0' * 64)
