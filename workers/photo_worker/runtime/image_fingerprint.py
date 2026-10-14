"""Decoded raster identity for operations that must preserve image content."""
from hashlib import sha256

from PIL import Image


def decoded_image_fingerprint(stream, mime, *, preserve_orientation=True,
                              check_active=lambda: None):
    check_active()
    stream.seek(0)
    if mime=='image/png':
        header=stream.read(33);stream.seek(0)
        if len(header)==33 and header[:8]==b'\x89PNG\r\n\x1a\n' and header[24]==16:
            return _png16_fingerprint(stream,preserve_orientation,check_active)
    if mime=='image/tiff':
        return _tiff_fingerprint(stream,preserve_orientation,check_active)
    with Image.open(stream) as image:
        expected = {'image/jpeg': 'JPEG', 'image/png': 'PNG',
                    'image/tiff': 'TIFF'}.get(mime)
        if (expected is None or image.format != expected
                or getattr(image, 'n_frames', 1) != 1
                or image.width < 1 or image.height < 1
                or image.width * image.height > 200_000_000):
            raise ValueError('IMAGE_FINGERPRINT_FORMAT')
        image.load()
        check_active()
        digest = sha256()
        for y in range(0, image.height, 256):
            check_active()
            with image.crop((0, y, image.width, min(y + 256, image.height))) as strip:
                digest.update(strip.tobytes())
                if image.mode == 'P':
                    with strip.convert('RGBA') as rgba:
                        digest.update(rgba.tobytes())
        return (image.size, image.mode, digest.hexdigest(),
                sha256(image.info.get('icc_profile', b'')).hexdigest(),
                image.getexif().get(274) if preserve_orientation else None,
                image.info.get('transparency'))


def _tiff_fingerprint(stream,preserve_orientation,check_active):
    """Keep every sample bit; Pillow RGB conversion can truncate uint16 TIFF."""
    import numpy as np
    import tifffile
    with Image.open(stream) as image:
        if (image.format!='TIFF' or getattr(image,'n_frames',1)!=1
                or image.width<1 or image.height<1 or image.width*image.height>200_000_000):
            raise ValueError('IMAGE_FINGERPRINT_FORMAT')
        size=image.size
        mode=image.mode
        icc=sha256(image.info.get('icc_profile',b'')).hexdigest()
        orientation=image.getexif().get(274) if preserve_orientation else None
        transparency=image.info.get('transparency')
        stream.seek(0)
        with tifffile.TiffFile(stream) as document:
            if len(document.pages)!=1:raise ValueError('IMAGE_FINGERPRINT_FORMAT')
            page=document.pages[0]
            if (page.shape[:2]!=(size[1],size[0]) or len(page.shape) not in (2,3)
                    or len(page.shape)==3 and page.shape[2] not in (1,2,3,4)
                    or page.dtype.kind not in ('u','i','f') or page.dtype.itemsize not in (1,2,4,8)):
                raise ValueError('IMAGE_FINGERPRINT_FORMAT')
            check_active()
            pixels=page.asarray()
            if pixels.shape!=page.shape or pixels.dtype!=page.dtype:raise ValueError('IMAGE_FINGERPRINT_FORMAT')
            digest=sha256()
            canonical_dtype=pixels.dtype.newbyteorder('<')
            for y in range(0,size[1],256):
                check_active()
                strip=np.ascontiguousarray(pixels[y:y+256].astype(canonical_dtype,copy=False))
                digest.update(strip.tobytes())
            check_active()
            palette=page.colormap
            palette_sha=(sha256(np.ascontiguousarray(palette.astype(palette.dtype.newbyteorder('<'),copy=False)).tobytes()).hexdigest()
                if palette is not None else None)
            return (size,(mode,canonical_dtype.str,pixels.shape,palette_sha),digest.hexdigest(),icc,orientation,transparency)


def _png16_fingerprint(stream,preserve_orientation,check_active):
    """Decode RGB/RGBA uint16 samples without Pillow's eight-bit conversion."""
    import cv2
    import numpy as np
    with Image.open(stream) as image:
        if (image.format!='PNG' or getattr(image,'n_frames',1)!=1
                or image.width<1 or image.height<1 or image.width*image.height>200_000_000):
            raise ValueError('IMAGE_FINGERPRINT_FORMAT')
        size=image.size
        icc=sha256(image.info.get('icc_profile',b'')).hexdigest()
        orientation=image.getexif().get(274) if preserve_orientation else None
        transparency=image.info.get('transparency')
        stream.seek(0)
        # A 200M-pixel RGBA16 raster fits below 2GiB before compression; refuse
        # unbounded ancillary payloads rather than allocating arbitrary bytes.
        chunks=[];count=0
        while block:=stream.read(1048576):
            check_active();count+=len(block)
            if count>2147483647:raise ValueError('IMAGE_FINGERPRINT_BYTES')
            chunks.append(block)
        packed=b''.join(chunks);del chunks
        check_active()
        pixels=cv2.imdecode(np.frombuffer(packed,dtype=np.uint8),cv2.IMREAD_UNCHANGED)
        del packed
        if (pixels is None or pixels.dtype!=np.uint16 or pixels.shape[:2]!=(size[1],size[0])
                or pixels.ndim not in (2,3) or pixels.ndim==3 and pixels.shape[2] not in (3,4)):
            raise ValueError('IMAGE_FINGERPRINT_FORMAT')
        digest=sha256()
        for y in range(0,size[1],256):
            check_active()
            digest.update(np.ascontiguousarray(pixels[y:y+256].astype('<u2',copy=False)).tobytes())
        check_active()
        return (size,('png16',pixels.shape),digest.hexdigest(),icc,orientation,transparency)
