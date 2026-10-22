#!/usr/bin/env python3
"""Explicit synthetic backend check; never enables a worker registration."""
from pathlib import Path
from dataclasses import asdict
import argparse,hashlib,json,platform,time
import numpy as np
import torch
from skimage.color import deltaE_ciede2000
from photo_worker.algorithms.fitting import PairSamples,FitOptions,fit_color,identity_color,reconstruction_metrics
from photo_worker.algorithms.color import apply_global,apply_local,rgb_to_lab_d50
from photo_worker.contracts.style import LocalColor

ROOT=Path(__file__).resolve().parents[1]

def main():
    parser=argparse.ArgumentParser()
    parser.add_argument('--backends',nargs='+',choices=('cpu','mps','cuda'),default=['cpu','mps'])
    parser.add_argument('--metal-memory-limit-bytes',type=int,default=None,
                        help='Optional operator ceiling; defaults to the device recommendation')
    args=parser.parse_args()
    if args.metal_memory_limit_bytes is not None and not 0<args.metal_memory_limit_bytes<=2**53-1:
        parser.error('Metal verification limit must be positive')
    torch.set_num_threads(2)
    rng=np.random.default_rng(2026)
    train=rng.uniform(.12,.72,(512,3)).astype(np.float32)
    holdout=rng.uniform(.12,.72,(256,3)).astype(np.float32)
    train_mask=np.r_[np.full(256,.9),np.full(256,.1)].astype(np.float32)
    holdout_mask=np.r_[np.full(128,.9),np.full(128,.1)].astype(np.float32)
    desired=identity_color().model_copy(update={'gains_rgb':[1.12,.94,1.04]})
    local=LocalColor(enabled=True,person_ev=.06,background_ev=-.04,person_saturation=1.02,background_saturation=.98,mask_policy_id='person_guided_v1')
    target=lambda x,mask:apply_local(apply_global(x,desired),mask,local)
    pair=PairSamples('backend-synthetic',train,target(train,train_mask),holdout,target(holdout,holdout_mask),train_mask,holdout_mask)
    baseline=reconstruction_metrics(identity_color(),[pair])
    report={'scope':'Synthetic fixed-fixture fitting only; not photographic/model qualification or worker registration approval',
        'torch_version':str(torch.__version__),'machine':platform.machine(),'system':platform.system(),
        'metal_memory_limit_bytes':args.metal_memory_limit_bytes,'fixture_seed':2026,'train_samples':512,'holdout_samples':256,'baseline':baseline,'backends':{},
        'source_sha256':{str(path.relative_to(ROOT)):hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted((ROOT/'workers/photo_worker/algorithms').glob('*.py'))}}
    rendered={}
    for backend in dict.fromkeys(args.backends):
        available=backend=='cpu' or backend=='mps' and torch.backends.mps.is_available() or backend=='cuda' and torch.cuda.is_available()
        if not available:
            report['backends'][backend]={'status':'unavailable'};continue
        if backend=='mps':
            recommended=torch.mps.recommended_max_memory()
            if recommended<=0:raise RuntimeError('Invalid Metal recommended memory')
            # Keep PyTorch's allocator guard; the device recommendation is the
            # default ceiling, with an optional lower operator override.
            metal_limit=min(recommended,args.metal_memory_limit_bytes or recommended)
            torch.mps.set_per_process_memory_fraction(min(1.0,metal_limit/recommended))
            report['metal_effective_limit_bytes']=metal_limit
        started=time.monotonic();deadline=started+900
        def active():
            if time.monotonic()>=deadline:raise RuntimeError('Qualification deadline exceeded')
            if backend=='mps' and torch.mps.driver_allocated_memory()>metal_limit:raise RuntimeError('Metal verification memory ceiling exceeded')
        try:
            options=FitOptions(device=backend)
            result=fit_color([pair],options,check_active=active)
            rendered[backend]=apply_local(apply_global(holdout,result.global_color),holdout_mask,result.local) if result.local.enabled else apply_global(holdout,result.global_color)
            passed=(result.metrics['median_delta_e']<baseline['median_delta_e']*.5 and result.metrics['p95_delta_e']<baseline['p95_delta_e']*.5)
            report['backends'][backend]={'status':'passed' if passed else 'failed_quality','elapsed_seconds':time.monotonic()-started,
                'options':asdict(options),'metrics':result.metrics,'local_enabled':result.local.enabled,
                'initialization_index':result.initialization_index,'history':result.history}
            if backend=='mps':report['backends'][backend]['driver_allocated_bytes_after']=torch.mps.driver_allocated_memory()
        except Exception as exc:
            report['backends'][backend]={'status':'failed_execution','error_type':type(exc).__name__,'elapsed_seconds':time.monotonic()-started}
        print(backend,report['backends'][backend]['status'],flush=True)
    for backend,output in rendered.items():
        if backend=='cpu' or 'cpu' not in rendered:continue
        delta=deltaE_ciede2000(rgb_to_lab_d50(output),rgb_to_lab_d50(rendered['cpu']))
        p95=float(np.percentile(delta,95))
        report['backends'][backend]['cpu_comparison_p95_delta_e']=p95
        if p95>.5:report['backends'][backend]['status']='failed_parity'
    path=ROOT/'work/evidence/fitting-backends.json'
    path.parent.mkdir(parents=True,exist_ok=True)
    path.write_text(json.dumps(report,indent=2)+'\n')
    failed=any(item['status']!='passed' for item in report['backends'].values())
    if failed:raise SystemExit(1)

if __name__=='__main__':main()
