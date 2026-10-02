"""Opt-in real >4GiB archive.build IO; generated data stays in the repository."""
from contextlib import contextmanager
from hashlib import sha256
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4
import os
import shutil
import zipfile

import pytest
from photo_worker.contracts.archive import ArchiveEntry,ArchiveParams
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.archive.build import execute
from photo_worker.runtime.scratch import ScratchSpace

ROOT=Path(__file__).resolve().parents[2]


def test_real_zip64_member_over_four_gib():
    if os.environ.get('PHOTO_RUN_ZIP64_LARGE_IO')!='1':
        pytest.skip('Set PHOTO_RUN_ZIP64_LARGE_IO=1 for actual >4GiB disk IO')
    size=2**32+1024*1024+17
    if shutil.disk_usage(ROOT).free<8*2**30:
        pytest.fail('ZIP64 qualification needs 8GiB free repository storage')
    block=sha256(b'PhotoEditor ZIP64 deterministic IO fixture').digest()*32768
    digest=sha256();remaining=size
    while remaining:
        count=min(remaining,len(block));digest.update(block[:count]);remaining-=count
    source_ref=ArtifactRef(artifact_id=uuid4(),sha256=digest.hexdigest())
    params=ArchiveParams(snapshot_id=uuid4(),snapshot_digest='d'*64,volume_no=1,
        entries=[ArchiveEntry(artifact=source_ref,bytes=size,entry_name='large.bin')])
    folder=ROOT/'.runtime/zip64-large-io'/uuid4().hex;folder.mkdir(parents=True)
    class Context:
        lease=SimpleNamespace(execution_profile={'backend':'cpu'},command=SimpleNamespace(inputs=[source_ref]))
        checks=0
        def check_active(self):self.checks+=1
        def progress(self,done,total):print(f'archive progress {done}/{total}',flush=True)
    class Source:
        remaining=size
        def read(self,count):
            count=min(count,self.remaining,len(block));self.remaining-=count
            return block[:count]
    class Artifacts:
        archive_bytes=0
        manifest=None
        scratch=ScratchSpace(folder,uuid4(),uuid4(),1,6*2**30,min_free_bytes=0)
        def params(self,model):return params
        @contextmanager
        def stream(self,requested):
            assert requested==source_ref
            yield Source()
        def stage(self,path,reservation,role,media):
            assert (role,media)==('archive','application/zip')
            reservation.validate_file(path)
            with path.open('rb') as source:
                os.fsync(source.fileno())
                source.seek(-1024,os.SEEK_END);footer=source.read()
            assert b'PK\x06\x06' in footer and b'PK\x06\x07' in footer
            self.archive_bytes=path.stat().st_size
            assert self.archive_bytes>2**32
            assert path.stat().st_blocks*512>=2**32  # Non-sparse actual disk writes.
            with zipfile.ZipFile(path) as archive:
                info=archive.getinfo('large.bin')
                assert info.file_size==size and info.extract_version>=45
                with archive.open(info) as source:
                    assert source.read(len(block))==block
            hashed=sha256()
            with path.open('rb') as source:
                for chunk in iter(lambda:source.read(1024*1024),b''):hashed.update(chunk)
            self.archive_sha=hashed.hexdigest()
            return ArtifactRef(artifact_id=uuid4(),sha256=self.archive_sha)
        def stage_contract(self,value,role):
            assert role=='archive_manifest'
            self.manifest=value
            return ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
    artifacts=Artifacts();ctx=Context()
    try:
        result=execute(ctx,artifacts)
        assert len(result.outputs)==2 and result.bytes_read==size
        assert artifacts.manifest.archive_bytes==artifacts.archive_bytes
        assert artifacts.manifest.archive_sha256==artifacts.archive_sha
        assert artifacts.manifest.entries[0].sha256==source_ref.sha256
        assert artifacts.manifest.entries[0].bytes==size
        assert ctx.checks>3*(size//len(block))
        print(f'ZIP64 verified: member={size}, archive={artifacts.archive_bytes}, active_checks={ctx.checks}',flush=True)
    finally:
        artifacts.scratch.cleanup()
        shutil.rmtree(folder)
