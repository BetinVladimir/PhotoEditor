import argparse
import json
import os
from pathlib import Path
import sys

from .config import Config, env_paths, load
from .io import atomic_json
from .models import ensure_models


def main():
    parser=argparse.ArgumentParser(description='Local full-resolution portrait pipeline')
    parser.add_argument('command',choices=['validate','analyze','render','setup-models','schema','studio','preview'])
    parser.add_argument('--resume',action='store_true',help='Resume the latest run only when its recipe matches')
    parser.add_argument('--image', help='Relative input path for preview')
    args=parser.parse_args()
    try:
        if args.command=='schema':
            print(json.dumps(Config.model_json_schema(),indent=2,ensure_ascii=False)); return 0
        if args.command=='setup-models':
            print(json.dumps(ensure_models(download=True),indent=2)); return 0
        source,dest,path=env_paths(); cfg=load(path)
        if args.command=='studio':
            from .studio import launch
            return launch(source,dest,path)
        from .jobs import emit, job_lock, make_plan, run_batch, scan
        if args.command=='validate':
            models=ensure_models(); files=scan(source,cfg)
            emit('validated',files=len(files),profile=cfg.profile,models=models); return 0
        with job_lock(dest):
            if args.command=='preview':
                if not args.image: raise ValueError('preview requires --image relative/path.jpg')
                from .jobs import ensure_plan
                from .config import for_image
                from .io import read_rgb, proxy_rgb, write_jpeg
                from .render import render
                import numpy as np
                plan=ensure_plan(source,dest,cfg)
                item=next((i for i in plan['items'] if i['relative']==args.image),None)
                if not item or item['status']!='ready': raise ValueError('Image is not ready for preview')
                rgb,_=read_rgb(source/item['relative'],cfg)
                proxy=proxy_rgb(rgb,cfg.export.preview_long_edge)
                with np.load(dest/'.cache'/f'{item["cache_key"]}.npz') as archive:
                    maps={k:archive[k] for k in archive.files}
                result,_=render(proxy,maps,item['stats'],for_image(cfg,item['relative']),item['exposure_ev'])
                output=dest/'.studio'/f'{item["source_id"]}.jpg'
                write_jpeg(output,np.round(result/257).astype(np.uint8),95)
                emit('preview_ready',path=str(output),kind='proxy')
                return 0
            if args.command=='analyze':
                plan=make_plan(source,dest,cfg)
                return 3 if any(i['status']=='failed' for i in plan['items']) else 0
            _,code=run_batch(source,dest,cfg,args.resume)
            return code
    except KeyboardInterrupt:
        print('Cancelled. Use render --resume to continue.',file=sys.stderr); return 4
    except (ValueError,OSError,ImportError) as exc:
        print(f'Configuration/environment error: {exc}',file=sys.stderr); return 2
    except Exception as exc:
        print(f'Processing error: {exc}',file=sys.stderr); return 5


if __name__=='__main__':
    sys.exit(main())
