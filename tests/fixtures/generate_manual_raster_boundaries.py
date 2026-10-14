"""Generate deterministic exact float32 samples using the production rasterizer.
Run from the repository: PYTHONPATH=workers .venv-platform/bin/python tests/fixtures/generate_manual_raster_boundaries.py
"""
import json
from pathlib import Path
import numpy as np
from photo_worker.contracts.manual_mask import ManualMask
from photo_worker.algorithms.manual_mask import rasterize

rng=np.random.default_rng(20261005)
results=[]
for case in range(36):
    w,h=(61,47) if case%2==0 else (47,61)
    if case>=32:w,h=(4097,3) if case%2==0 else (3,4097)
    canvas=dict(width=w,height=h,orientation=1,space_id='oriented:boundary',source_sha256='a'*64)
    shapes=[]
    for ordinal in range(4):
        mode=('add','subtract','protect','add')[ordinal]
        points=rng.random((3,2)).tolist()
        if case%4==0:points[1]=points[0].copy()
        if case%4==1:points[0]=[0.,1.];points[-1]=[1.,0.]
        hardness=[0.,1.,np.nextafter(1.,0.),np.nextafter(0.,1.)][case%4]
        radius=[.5/max(w,h),np.nextafter(1/max(w,h),0.),np.nextafter(1/max(w,h),1.),.37][case%4]
        shape=dict(kind='stroke',sequence=ordinal,mode=mode,points=[dict(x=x,y=y) for x,y in points],radius=float(radius),hardness=float(hardness))
        if ordinal==3 and case%2==0:
            shape=dict(kind='polygon',sequence=ordinal,mode=mode,points=[dict(x=0.,y=.5),dict(x=1.,y=.5),dict(x=.5,y=np.nextafter(.5,1.))])
        shapes.append(shape)
    rng.shuffle(shapes)
    mask=ManualMask.model_validate(dict(source_sha256='a'*64,canvas=canvas,shapes=shapes))
    target=np.zeros((h,w),np.float32);protected=np.zeros_like(target)
    for tile in rasterize(mask,'a'*64,tile_size=256):
        th,tw=tile.target.shape
        target[tile.y:tile.y+th,tile.x:tile.x+tw]=tile.target
        protected[tile.y:tile.y+th,tile.x:tile.x+tw]=tile.protected
    results.append(dict(vectors=mask.model_dump(mode='json'),target_bits=target.view(np.uint32).ravel().tolist(),protected_bits=protected.view(np.uint32).ravel().tolist()))
path=Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/manual-raster-boundaries-python.json'
path.write_text(json.dumps(results,separators=(',',':'))+'\n')
