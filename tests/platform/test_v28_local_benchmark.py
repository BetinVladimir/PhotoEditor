"""Benchmark must detect actual output corruption before emitting evidence."""
import importlib.util
from contextlib import contextmanager
from pathlib import Path
import sys
import numpy as np
import pytest


@pytest.mark.parametrize('fault',['protected','nonfinite','changed_mask'])
def test_local_benchmark_refuses_corrupt_prepared_output(monkeypatch,fault):
    path=Path(__file__).resolve().parents[2]/'tools/benchmark_v28_local.py'
    spec=importlib.util.spec_from_file_location('v28_measurement',path)
    tool=importlib.util.module_from_spec(spec);spec.loader.exec_module(tool)
    original=tool.prepare_local
    @contextmanager
    def corrupt(*args,**kwargs):
        with original(*args,**kwargs) as files:
            array=np.load(files.changed_mask if fault=='changed_mask' else files.rgb,mmap_mode='r+',allow_pickle=False)
            try:
                if fault=='nonfinite': array[0,0,0]=np.nan
                elif fault=='protected': array[0,10,0]+=1  # x=.5: protected stripe
                else: array[0,0]=1  # disabled pixel: expected unchanged
                array.flush()
            finally: array._mmap.close()
            yield files
    monkeypatch.setattr(tool,'prepare_local',corrupt)
    monkeypatch.setattr(sys,'argv',[str(path),'--width','20','--height','20'])
    with pytest.raises(RuntimeError,match='preparation invariants failed'):
        tool.main()

@pytest.mark.parametrize('operation',['portrait.hair','portrait.clothing'])
@pytest.mark.parametrize('fault',['','digest','path','mask_binding','dtype','icc'])
def test_pinned_benchmark_fixture_contract(tmp_path,operation,fault):
    import json
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import canonical_bytes,canonical_hash
    path=Path(__file__).resolve().parents[2]/'tools/benchmark_v28_local.py'
    spec=importlib.util.spec_from_file_location('v28_fixture_measurement',path)
    tool=importlib.util.module_from_spec(spec);spec.loader.exec_module(tool)
    manifest={'schema_version':1,'operation':operation}
    def write(name,value,binary=False):
        file=tmp_path/(name+('.npy' if binary else '.json'))
        if binary:np.save(file,value,allow_pickle=False)
        else:file.write_bytes(canonical_bytes(value))
        manifest[name]={'path':file.name,'sha256':sha256(file.read_bytes()).hexdigest(),'artifact_id':str(uuid4())}
        return {k:manifest[name][k] for k in ('artifact_id','sha256')}
    rgb=write('rgb',np.full((20,20,3),.4,dtype=np.float64 if fault=='dtype' else np.float32),True)
    target=write('target',np.full((20,20),.8,np.float32),True)
    guard=write('protected',np.zeros((20,20),np.float32),True)
    root=path.parents[1]
    image=json.loads((root/'backend/internal/review/testdata/working-images-python.json').read_bytes())[0]
    image['pixels']=rgb;image['canvas'].update(width=20,height=20)
    from photo_worker.color.icc import ICCRegistry
    image['color']['icc_sha256']=ICCRegistry(root/'resources/icc').resolve(image['color']['profile_id']).sha256
    if fault=='icc':image['color']['icc_sha256']='0'*64
    source=write('working_image',image)
    masks=json.loads((root/'backend/internal/review/testdata/face-manual-root-python.json').read_bytes())
    for name,mask,pin in [('target_ref',masks['target'],target),('protected_ref',masks['protected'],guard)]:
        mask['artifact']=pin;mask['canvas']=dict(image['canvas'])
        if fault=='mask_binding' and name=='target_ref':mask['canvas']['width']=21
        write(name,mask)
    ref=lambda name:{k:manifest[name][k] for k in ('artifact_id','sha256')}
    params={'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':source,'canvas_hash':canonical_hash(image['canvas'])},'mask':ref('target_ref'),'protected':ref('protected_ref'),'strength':.7,'hue_delta_degrees':20,'saturation_factor':1.2,('detail' if operation=='portrait.hair' else 'wrinkle_strength'):.8}
    write('params',params)
    if fault=='digest':manifest['rgb']['sha256']='0'*64
    if fault=='path':manifest['rgb']['path']='../outside.npy'
    raw=canonical_bytes(manifest);file=tmp_path/'manifest.json';file.write_bytes(raw)
    snapshots=tmp_path/'snapshots';snapshots.mkdir()
    if fault:
        with pytest.raises(ValueError,match='FIXTURE_'):
            tool.load_fixture(tmp_path,snapshots,file,sha256(raw).hexdigest(),operation)
    else:
        arrays,color,parsed,w,h=tool.load_fixture(tmp_path,snapshots,file,sha256(raw).hexdigest(),operation)
        try:
            assert (w,h)==(20,20) and parsed.strength==.7
            np.testing.assert_array_equal(arrays[0],np.full((20,20,3),.4,np.float32))
            assert all(not array.flags.writeable for array in arrays)
        finally:
            for array in arrays:array._mmap.close()

        # Exercise the real CLI using a repository-contained corpus, independently
        # of pytest's temporary-directory location. Preserve original input bytes.
        import tempfile
        import shutil
        import subprocess
        import os
        with tempfile.TemporaryDirectory(prefix='.v28-fixture-cli-',dir=root) as corpus:
            corpus=Path(corpus)
            for entry in manifest.values():
                if isinstance(entry,dict):
                    destination=corpus/Path(entry['path']).name
                    shutil.copyfile(tmp_path/entry['path'],destination)
                    entry['path']=str(destination)
            wire=canonical_bytes(manifest);manifest_file=corpus/'manifest.json';manifest_file.write_bytes(wire)
            pin=sha256(wire).hexdigest()
            original={entry['path']:Path(entry['path']).read_bytes() for entry in manifest.values() if isinstance(entry,dict)}
            reports=[]
            for repeat in range(2):
                result=subprocess.run([sys.executable,str(path),'--operation',operation,'--fixture',str(manifest_file),'--fixture-sha256',pin],cwd=root,env={**os.environ,'PYTHONPATH':str(root/'workers')},capture_output=True,text=True,timeout=30)
                assert result.returncode==0,result.stdout+result.stderr
                report=json.loads(result.stdout);reports.append(report)
                assert report['input_pattern']=='pinned_fixture' and report['fixture_sha256']==pin
                assert (report['width'],report['height'])==(20,20) and report['production_acceptance'] is False
                assert not any(report['checks'][key] for key in ('nonfinite_values','protected_pixel_changes','disabled_pixel_changes','changed_mask_mismatches'))
            assert reports[0]['output_pixels_sha256']==reports[1]['output_pixels_sha256']
            assert all(Path(name).read_bytes()==body for name,body in original.items())
