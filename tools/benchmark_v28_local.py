"""Local CPU preparation measurement; not production/hardware acceptance.

Run from repo: PYTHONPATH=workers .venv-platform/bin/python tools/benchmark_v28_local.py
Temporary inputs/output remain inside this repository and are removed on exit.
"""
import argparse
import json
from pathlib import Path
import platform
import resource
import shutil
import hashlib
import tempfile
import time
from uuid import uuid4

import numpy as np

from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.contracts.base import canonical_hash,parse_json
from photo_worker.contracts.portrait_local import PortraitClothingParams,PortraitHairParams
from photo_worker.operations.portrait.local_resources import local_portrait_resources
from photo_worker.operations.portrait.prepare_local import prepare_local
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.color.icc import ICCRegistry


def load_fixture(root, directory, path, expected_sha, operation):
    """Snapshot pinned corpus files inside the repo before parsing or mmap IO.

    This verifies local measurement inputs, not catalog authority or human QA.
    """
    def contained(path):
        result=(root/path).resolve()
        if not result.is_relative_to(root) or not result.is_file():
            raise ValueError('FIXTURE_PATH_INVALID')
        return result
    with contained(Path(path)).open('rb') as stream:
        raw=stream.read(65537)
    if len(raw)>65536 or hashlib.sha256(raw).hexdigest()!=expected_sha:
        raise ValueError('FIXTURE_DIGEST_MISMATCH')
    manifest=parse_json(raw,max_bytes=65536)
    names=('working_image','params','target_ref','protected_ref','rgb','target','protected')
    if (set(manifest)!={'schema_version','operation',*names} or manifest['schema_version']!=1 or manifest['operation']!=operation):
        raise ValueError('FIXTURE_MANIFEST_INVALID')
    snapshots={}
    for name in names:
        entry=manifest[name]
        if not isinstance(entry,dict) or set(entry)!={'path','sha256','artifact_id'}:
            raise ValueError('FIXTURE_ENTRY_INVALID')
        source=contained(Path(entry['path']));destination=directory/(name+'.fixture')
        digest=hashlib.sha256();size=0
        limit=1048576 if name in names[:4] else 2400065548
        with source.open('rb') as incoming,destination.open('xb') as outgoing:
            for block in iter(lambda:incoming.read(1048576),b''):
                size+=len(block)
                if size>limit:raise ValueError('FIXTURE_FILE_TOO_LARGE')
                digest.update(block);outgoing.write(block)
        if digest.hexdigest()!=entry['sha256']:raise ValueError('FIXTURE_DIGEST_MISMATCH')
        snapshots[name]=destination
    image=WorkingImage.model_validate_json(snapshots['working_image'].read_bytes())
    profile=ICCRegistry(Path(__file__).resolve().parents[1]/'resources/icc').resolve(image.color.profile_id)
    if profile.sha256!=image.color.icc_sha256:
        raise ValueError('FIXTURE_ICC_BINDING_INVALID')
    model=PortraitHairParams if operation=='portrait.hair' else PortraitClothingParams
    params=model.model_validate_json(snapshots['params'].read_bytes())
    masks=[MaskRef.model_validate_json(snapshots[name].read_bytes()) for name in ('target_ref','protected_ref')]
    def matches(ref,name):
        entry=manifest[name]
        return str(ref.artifact_id)==entry['artifact_id'] and ref.sha256==entry['sha256']
    if (image.dtype!='float32' or image.alpha is not None or not matches(params.base.source,'working_image')
            or params.base.canvas_hash!=canonical_hash(image.canvas)
            or not matches(image.pixels,'rgb') or not matches(params.mask,'target_ref') or not matches(params.protected,'protected_ref')):
        raise ValueError('FIXTURE_BINDING_INVALID')
    for mask,name,protected in zip(masks,('target','protected'),(False,True)):
        if mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain or mask.protected!=protected or not matches(mask.artifact,name):
            raise ValueError('FIXTURE_MASK_BINDING_INVALID')
    arrays=[]
    try:
        h,w=image.canvas.height,image.canvas.width
        for name,shape in [('rgb',(h,w,3)),('target',(h,w)),('protected',(h,w))]:
            array=np.load(snapshots[name],mmap_mode='r',allow_pickle=False)
            arrays.append(array)
            if array.dtype!=np.dtype('<f4') or array.shape!=shape:raise ValueError('FIXTURE_ARRAY_INVALID')
            for y in range(0,h,256):
                block=array[y:y+256]
                if not np.isfinite(block).all() or name!='rgb' and (np.any(block<0) or np.any(block>1)):
                    raise ValueError('FIXTURE_ARRAY_INVALID')
        return arrays,image.color,params,w,h
    except Exception:
        for array in arrays:
            if getattr(array,'_mmap',None) is not None:array._mmap.close()
        raise


def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--fixture',type=Path)
    parser.add_argument('--fixture-sha256')
    parser.add_argument('--operation',choices=('portrait.hair','portrait.clothing'),default='portrait.clothing')
    parser.add_argument('--width',type=int,default=3000)
    parser.add_argument('--height',type=int,default=2000)
    parser.add_argument('--pattern',choices=('constant','waves'),default='waves')
    args=parser.parse_args()
    if bool(args.fixture)!=bool(args.fixture_sha256):parser.error("--fixture and --fixture-sha256 are required together")
    w,h=args.width,args.height
    required=local_portrait_resources(w,h)
    root=Path(__file__).resolve().parents[1]
    needed=52*w*h+256*1024*1024
    if shutil.disk_usage(root).free<needed:
        raise SystemExit('Insufficient repository disk space for benchmark')
    fixture=json.loads((root/'backend/internal/review/testdata/working-images-python.json').read_bytes())
    color=WorkingImage.model_validate_json(json.dumps(fixture[0])).color
    ref={'artifact_id':str(uuid4()),'sha256':'a'*64}
    model=PortraitHairParams if args.operation=='portrait.hair' else PortraitClothingParams
    params=model.model_validate_json(json.dumps({'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref,'canvas_hash':'b'*64},'strength':.7,'mask':ref,'protected':ref,'hue_delta_degrees':20,'saturation_factor':1.2,('detail' if args.operation=='portrait.hair' else 'wrinkle_strength'):.8}))
    with tempfile.TemporaryDirectory(prefix='.v28-benchmark-',dir=root) as directory:
        directory=Path(directory)
        arrays=[]
        try:
            if args.fixture:
                arrays,color,params,w,h=load_fixture(root,directory,args.fixture,args.fixture_sha256,args.operation)
                required=local_portrait_resources(w,h)
                if shutil.disk_usage(root).free<required['scratch_bytes']:
                    raise ValueError('Insufficient repository disk space for benchmark')
            else:
                for name,shape,value in [('rgb',(h,w,3),.4),('target',(h,w),.8),('protected',(h,w),0.)]:
                    mm=np.lib.format.open_memmap(directory/(name+'.npy'),mode='w+',dtype='<f4',shape=shape)
                    arrays.append(mm)
                    for y in range(0,h,256):
                        end=min(h,y+256)
                        if args.pattern=='constant': mm[y:end]=value
                        else:
                            yy=np.arange(y,end,dtype=np.float32)[:,None]/h
                            xx=np.arange(w,dtype=np.float32)[None,:]/w
                            if name=='rgb':
                                for channel in range(3):
                                    mm[y:end,:,channel]=.4+.15*np.sin((xx*(3+channel)+yy*(2+channel))*2*np.pi)
                            elif name=='target': mm[y:end]=np.where(xx<.1,0.,.2+.7*xx)
                            else: mm[y:end]=np.where((xx>.45)&(xx<.55),1.,0.)
                    mm.flush()
            scratch_root=directory/'scratch';scratch_root.mkdir()
            start=time.monotonic()
            with ScratchSpace(scratch_root,uuid4(),uuid4(),1,required['scratch_bytes'],min_free_bytes=0) as scratch:
                with prepare_local(params,*arrays,color,scratch,{'renderer_version':'v2.2','backend':'cpu','resource_estimate':required}) as files:
                    files.reservation.validate()
                    elapsed=time.monotonic()-start
                    output=np.load(files.rgb,mmap_mode='r',allow_pickle=False)
                    changed=np.load(files.changed_mask,mmap_mode='r',allow_pickle=False)
                    checks={'nonfinite_values':0,'protected_pixel_changes':0,'disabled_pixel_changes':0,'changed_mask_mismatches':0,'protected_pixels':0,'disabled_pixels':0}
                    digest=hashlib.sha256()
                    try:
                        for y in range(0,h,256):
                            end=min(h,y+256);actual=output[y:end];original=arrays[0][y:end]
                            guard=arrays[2][y:end]==1;disabled=arrays[1][y:end]==0
                            different=np.any(actual.view(np.uint32)!=original.view(np.uint32),axis=-1)
                            checks['nonfinite_values']+=int(np.count_nonzero(~np.isfinite(actual)))
                            checks['protected_pixels']+=int(np.count_nonzero(guard))
                            checks['disabled_pixels']+=int(np.count_nonzero(disabled))
                            checks['protected_pixel_changes']+=int(np.count_nonzero(different & guard))
                            checks['disabled_pixel_changes']+=int(np.count_nonzero(different & disabled))
                            checks['changed_mask_mismatches']+=int(np.count_nonzero(changed[y:end]!=np.any(actual!=original,axis=-1).astype(np.float32)))
                            digest.update(actual.tobytes(order='C'))
                    finally:
                        output._mmap.close();changed._mmap.close()
                    if any(checks[k] for k in ('nonfinite_values','protected_pixel_changes','disabled_pixel_changes','changed_mask_mismatches')):
                        raise RuntimeError('V28 local preparation invariants failed: '+json.dumps(checks,sort_keys=True))
            peak=resource.getrusage(resource.RUSAGE_SELF).ru_maxrss
            if platform.system()!='Darwin': peak*=1024
            print(json.dumps({'schema_version':1,'scope':'local_cpu_preparation_only','operation':args.operation,'input_pattern':'pinned_fixture' if args.fixture else args.pattern,'fixture_sha256':args.fixture_sha256,'width':w,'height':h,'seconds':elapsed,'checks':checks,'output_pixels_sha256':digest.hexdigest(),'process_peak_rss_bytes':peak,'os':platform.system(),'python':platform.python_version(),'numpy':np.__version__,'admitted_estimate':required,'production_acceptance':False},sort_keys=True))
        finally:
            for mm in arrays: mm._mmap.close()


if __name__=='__main__': main()
