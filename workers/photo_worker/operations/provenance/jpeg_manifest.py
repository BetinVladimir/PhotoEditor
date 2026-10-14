"""JPEG APP11 exact C2PA store binding; not COSE/signature validation.

Fragment layout follows contentauth/c2pa-rs jpeg_io.rs / ISO 19566-5.
"""
from struct import unpack


def verify_jpeg_manifest(stream, manifest, *, check_active=lambda: None):
    if (not isinstance(manifest, bytes) or not 28 <= len(manifest) <= 8 << 20
            or manifest[4:8] != b'jumb' or manifest[16:20] != b'c2pa'
            or not callable(check_active)):
        raise ValueError('SIGNER_JPEG_MANIFEST_INPUT')
    box_size = unpack('>I', manifest[:4])[0]
    if box_size != len(manifest):
        raise ValueError('SIGNER_JPEG_BOX_SIZE')
    def guard():
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
    guard()
    position = stream.tell()
    instance, offset, sequence, contiguous = None, 0, 0, False
    segments = 0
    def read_exact(size):
        guard()
        if stream.tell()+size > 512 << 20:
            raise ValueError('SIGNER_JPEG_FILE_LIMIT')
        data=stream.read(size)
        guard()
        if not isinstance(data,bytes) or len(data)!=size:
            raise ValueError('SIGNER_JPEG_TRUNCATED')
        return data
    def entropy_marker():
        # Skip compressed scan bytes in bounded blocks. FF00 is byte stuffing;
        # restart markers remain inside the scan, other markers end the scan.
        while True:
            guard()
            start=stream.tell()
            if start >= 512 << 20:
                raise ValueError('SIGNER_JPEG_FILE_LIMIT')
            block=stream.read(min(1<<16,(512<<20)-start))
            guard()
            if not block:
                raise ValueError('SIGNER_JPEG_TRUNCATED')
            index=0
            while True:
                index=block.find(b'\xff',index)
                if index<0:break
                end=index+1
                while end<len(block) and block[end]==255:end+=1
                if end==len(block):
                    stream.seek(start+index)
                    # Deal with fill bytes across block boundaries without
                    # repeatedly rereading an all-FF block.
                    read_exact(1)
                    code=read_exact(1)[0]
                    while code==255:code=read_exact(1)[0]
                    if code==0 or 0xd0<=code<=0xd7:break
                    return code
                code=block[end]
                if code!=0 and not 0xd0<=code<=0xd7:
                    stream.seek(start+end+1)
                    return code
                index=end+1
    try:
        stream.seek(0)
        if read_exact(2)!=b'\xff\xd8':
            raise ValueError('SIGNER_JPEG_SIGNATURE')
        pending=None
        while True:
            segments+=1
            if segments>100000:
                raise ValueError('SIGNER_JPEG_SEGMENT_LIMIT')
            if pending is None:
                if read_exact(1)!=b'\xff':raise ValueError('SIGNER_JPEG_MARKER')
                code=read_exact(1)[0]
                while code==255:code=read_exact(1)[0]
            else:
                code,pending=pending,None
            if code==0xd9:
                guard()
                trailing=stream.read(1)
                guard()
                if trailing!=b'':raise ValueError('SIGNER_JPEG_TRAILING_DATA')
                if instance is None or offset!=len(manifest):
                    raise ValueError('SIGNER_JPEG_MANIFEST_MISSING')
                return
            if code in (0,0xd8) or 0xd0<=code<=0xd7:
                raise ValueError('SIGNER_JPEG_MARKER')
            if code==1:
                contiguous=False
                continue
            size=unpack('>H',read_exact(2))[0]
            if size<2:raise ValueError('SIGNER_JPEG_SEGMENT_SIZE')
            data=read_exact(size-2)
            is_fragment=False
            if code==0xeb and len(data)>=16 and data[:2]==b'JP':
                en,z=data[2:4],unpack('>I',data[4:8])[0]
                first=len(data)>=28 and data[24:28]==b'c2pa'
                if instance is not None and en==instance:
                    if not contiguous or z!=sequence+1 or data[8:16]!=manifest[:8]:
                        raise ValueError('SIGNER_JPEG_FRAGMENT_SEQUENCE')
                    fragment=data[16:]
                    is_fragment=True
                elif first:
                    if instance is not None or z!=1:
                        raise ValueError('SIGNER_JPEG_DUPLICATE_STORE')
                    instance=en
                    fragment=data[8:]
                    is_fragment=True
                if is_fragment:
                    if not fragment or offset+len(fragment)>len(manifest) or fragment!=manifest[offset:offset+len(fragment)]:
                        raise ValueError('SIGNER_JPEG_MANIFEST_BINDING')
                    offset+=len(fragment)
                    sequence=z
            contiguous=is_fragment
            if code==0xda:
                pending=entropy_marker()
    finally:
        stream.seek(position)
