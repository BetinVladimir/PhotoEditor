"""Owned depth-layer background blur with the existing native algorithm."""
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
import numpy as np
from photo_worker.algorithms.background import blur_background_tile, depth_layer_radii
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import Reservation

@dataclass(frozen=True)
class PreparedBlur:
    rgb: Path
    reservation: Reservation
    subject_depth: float
    radii: tuple[int, ...]

@contextmanager
def prepare_blur(rgb,subject,depth,scratch,*,strength,tile=512,
                 check_active=lambda:None,progress=lambda done:None):
    """Caller proves source/depth/mask authority and complete memory admission."""
    check_active()
    if (subject.ndim!=2 or type(tile) is not int or tile not in (256,512,1024)
            or isinstance(strength,bool) or not 0<=strength<=1):
        raise ValueError('BLUR_SOURCE_INVALID')
    height,width=subject.shape
    if (not 1<=height*width<=200000000 or rgb.shape!=(height,width,3)
            or depth.shape!=(height,width) or any(value.dtype!=np.float32 for value in (rgb,subject,depth))):
        raise ValueError('BLUR_SOURCE_INVALID')
    # Deterministic bounded reduction; no full-size temporary.
    weighted = 0.0; weight = 0.0
    for y in range(0, height, tile):
        check_active()
        mask = subject[y:y + tile]
        selected = mask >= .5
        if np.any(selected):
            weighted += float(np.sum(depth[y:y + tile][selected], dtype=np.float64))
            weight += int(np.count_nonzero(selected))
    if weight == 0:
        raise OperationFailure("MASK_EMPTY")
    subject_depth = weighted / weight
    radii = depth_layer_radii(max(height, width), strength, subject_depth)
    # OpenCV float Gaussian auto-kernel extends beyond radius (sigma=radius/3).
    # Retain the complete support so tile borders never affect their cores.
    halo = max((int(np.ceil(4 * max(radius / 3.0, .5))) + 2
                for radius in radii if radius > 0), default=0)
    progress(1)
    with scratch.reserve(height * width * 12 + 8192) as reservation:
        path = reservation.path("background-blur-rgb", ".npy")
        target = np.lib.format.open_memmap(path, mode="w+", dtype="<f4", shape=(height, width, 3))
        try:
            done = 1
            for y in range(0, height, tile):
                for x in range(0, width, tile):
                    check_active()
                    y0, y1 = max(0, y - halo), min(height, y + tile + halo)
                    x0, x1 = max(0, x - halo), min(width, x + tile + halo)
                    core = (slice(y - y0, min(y + tile, height) - y0), slice(x - x0, min(x + tile, width) - x0))
                    target[y:min(y + tile, height), x:min(x + tile, width)] = blur_background_tile(
                        rgb[y0:y1, x0:x1], subject[y0:y1, x0:x1], depth[y0:y1, x0:x1], radii, core)
                    done += 1; progress(done)
            target.flush(); reservation.validate()
        finally:
            target._mmap.close()
        check_active()
        yield PreparedBlur(path,reservation,subject_depth,tuple(radii))
        check_active()
