"""APP11 reconstruction fixtures; not C2PA cryptographic credentials."""
from io import BytesIO
from struct import pack

import pytest

from photo_worker.operations.provenance.jpeg_manifest import verify_jpeg_manifest


def store(size=100):
    return pack('>I',size)+b'jumb'+pack('>I',24)+b'jumd'+b'c2pa'+b'x'*(size-20)


def segment(code,data):
    return b'\xff'+bytes([code])+pack('>H',len(data)+2)+data


def fragments(manifest, split=60):
    parts=[]
    for number,offset in enumerate(range(0,len(manifest),split),1):
        body=manifest[offset:offset+split]
        parts.append(segment(0xeb,b'JP\x02\x11'+pack('>I',number)
            +(manifest[:8] if number>1 else b'')+body))
    return parts


def jpeg(parts):
    return (b'\xff\xd8'+b''.join(parts)+segment(0xda,b'fixture-scan-header')
        +b'compressed\xff\x00stuffed\xff\xd0restart\xff\xd9')


@pytest.mark.parametrize('size,split',[(100,100),(100,60),(100000,60000)])
def test_exact_fragmented_store_and_position_restored(size,split):
    manifest=store(size)
    stream=BytesIO(jpeg(fragments(manifest,split)))
    stream.seek(3)
    verify_jpeg_manifest(stream,manifest)
    assert stream.tell()==3


@pytest.mark.parametrize('mode',['changed','order','gap','duplicate','truncated','trailing','missing'])
def test_fragment_or_container_mismatch_refused(mode):
    manifest=store()
    parts=fragments(manifest)
    if mode=='changed':parts[-1]=parts[-1][:-1]+b'z'
    if mode=='order':parts=parts[::-1]
    if mode=='gap':parts.insert(1,segment(0xe1,b'foreign'))
    if mode=='duplicate':parts+=fragments(manifest)
    if mode=='missing':parts=[]
    raw=jpeg(parts)
    if mode=='truncated':raw=raw[:-2]
    if mode=='trailing':raw+=b'x'
    stream=BytesIO(raw)
    stream.seek(1)
    with pytest.raises(ValueError):verify_jpeg_manifest(stream,manifest)
    assert stream.tell()==1


def test_entropy_boundary_stuffing_and_progressive_scans():
    manifest=store()
    raw=(b'\xff\xd8'+b''.join(fragments(manifest))+segment(0xda,b'x')
        +b'x'*65535+b'\xff\x00'+b'next'+segment(0xda,b'y')
        +b'\xff\xff\xd1'+b'last\xff\xd9')
    verify_jpeg_manifest(BytesIO(raw),manifest)


def test_cancelled_scan_restores_position():
    manifest=store()
    stream=BytesIO(jpeg(fragments(manifest)))
    stream.seek(2)
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==6:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):verify_jpeg_manifest(stream,manifest,check_active=active)
    assert stream.tell()==2


def test_boolean_cancellation_during_final_eof_refuses_manifest():
    live=True
    class Stream(BytesIO):
        def read(self,size=-1):
            nonlocal live
            result=super().read(size)
            if size==1 and result==b'':live=False
            return result
    manifest=store();stream=Stream(jpeg(fragments(manifest)));stream.seek(3)
    with pytest.raises(RuntimeError,match='SIGNER_TASK_INACTIVE'):
        verify_jpeg_manifest(stream,manifest,check_active=lambda:live)
    assert stream.tell()==3
