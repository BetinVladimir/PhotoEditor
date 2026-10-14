"""Exact container binding only; fixtures are not signed C2PA credentials."""
from io import BytesIO
from struct import pack
from zlib import crc32

import pytest

from photo_worker.operations.provenance.png_manifest import PNG_SIGNATURE, verify_png_manifest


def chunk(kind, data):
    return pack('>I',len(data))+kind+data+pack('>I',crc32(kind+data)&0xffffffff)


def png(store=b'manifest'):
    return (PNG_SIGNATURE+chunk(b'IHDR',b'\0'*13)+chunk(b'caBX',store)
            +chunk(b'IDAT',b'fixture-image')+chunk(b'IEND',b''))


def test_exact_store_and_position_restored():
    stream=BytesIO(png())
    stream.seek(5)
    verify_png_manifest(stream,b'manifest')
    assert stream.tell()==5


@pytest.mark.parametrize('mode',['changed','duplicate','missing','crc','truncated','trailing','header'])
def test_container_or_store_mismatch_refused(mode):
    raw=png()
    if mode=='changed':raw=png(b'different')
    if mode=='duplicate':raw=raw[:-12]+chunk(b'caBX',b'manifest')+chunk(b'IEND',b'')
    if mode=='missing':raw=PNG_SIGNATURE+chunk(b'IHDR',b'\0'*13)+chunk(b'IEND',b'')
    if mode=='crc':raw=raw[:-1]+bytes([raw[-1]^1])
    if mode=='truncated':raw=raw[:-3]
    if mode=='trailing':raw+=b'x'
    if mode=='header':raw=PNG_SIGNATURE+chunk(b'IDAT',b'x')+chunk(b'IEND',b'')
    stream=BytesIO(raw)
    stream.seek(2)
    with pytest.raises(ValueError):verify_png_manifest(stream,b'manifest')
    assert stream.tell()==2


def test_cancellation_during_scan_restores_position():
    stream=BytesIO(png())
    stream.seek(3)
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==5:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):verify_png_manifest(stream,b'manifest',check_active=active)
    assert stream.tell()==3


def test_boolean_cancellation_during_final_eof_refuses_manifest():
    live=True
    class Stream(BytesIO):
        def read(self,size=-1):
            nonlocal live
            result=super().read(size)
            if size==1 and result==b'':live=False
            return result
    stream=Stream(png());stream.seek(4)
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        verify_png_manifest(stream,b'manifest',check_active=lambda:live)
    assert stream.tell()==4


def test_large_manifest_is_compared_in_bounded_reads():
    manifest=b'x'*((1<<20)+17)
    class Stream(BytesIO):
        def read(self,size=-1):
            assert 0<=size<=1<<20
            return super().read(size)
    verify_png_manifest(Stream(png(manifest)),manifest)
