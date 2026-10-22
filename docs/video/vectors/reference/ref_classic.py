import numpy as np, subprocess, json, sys
sys.path.insert(0,'.')
from ref_cuts import postprocess
from ref_f1 import score, prf
FT=28_224_000
def load_frames(path):
    raw=subprocess.run(['ffmpeg','-v','error','-nostdin','-i',path,'-vf','scale=48:27:flags=area','-f','rawvideo','-pix_fmt','rgb24','-'],capture_output=True,check=True).stdout
    return np.frombuffer(raw,np.uint8).reshape(-1,27,48,3)
def features(fr):
    n=len(fr)
    H=np.stack([np.stack([np.histogram(fr[i][...,c],bins=16,range=(0,256))[0] for c in range(3)]) for i in range(n)]).astype(np.float64)/(27*48)
    d=np.array([0.5*np.abs(H[i]-H[i+1]).sum(axis=1).mean() for i in range(n-1)])
    luma=16.0+(219.0/255.0)*(fr.astype(np.float64).mean(axis=(1,2))@np.array([0.299,0.587,0.114]))
    # single[i]: spike of d[i] over neighbours i-2..i+2 (excluding i)
    single=np.zeros(n)
    many=np.zeros(n)
    for i in range(n-1):
        nb=[d[j] for j in range(max(0,i-2),min(n-1,i+3)) if j!=i]
        s=d[i]-(max(nb) if nb else 0.0)
        single[i]=min(max(s/0.7,0.0),1.0)
    # many[i]: mean of d over i-2..i+2 / 0.4, zeroed when luma ramps (|luma[i+2]-luma[i-2]|>24)
    for i in range(n):
        lo=max(0,i-2);hi=min(n-1,i+2)
        w=d[lo:min(hi,n-2)+1]
        m=float(w.mean()) if len(w) else 0.0
        many[i]=min(m/0.4,1.0)
        if abs(luma[hi]-luma[lo])>24 or min(luma[max(0,i-3):min(n-1,i+3)+1])<=24: many[i]=0.0
    return single.tolist(),many.tolist(),luma.tolist()
if __name__=='__main__':
    out={}
    for f in ('hard_cuts','dissolve','fades'):
        fr=load_frames(f'../fx2/{f}.mp4');s,m,l=features(fr)
        b=postprocess(s,m,l,sens=3,fps='25/1')
        print(f,len(fr),[(x['kind'],x['frame_in'],x['frame_out'],x['confidence']) for x in b])
        out[f]=b
    json.dump(out,open('classic_fixture_out.json','w'),indent=1)
