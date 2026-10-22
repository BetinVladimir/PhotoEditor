import json, math
MiB=1<<20;GiB=1<<30;TPS=705_600_000
def part_bytes(b):
    raw=-(-b//9000);r=-(-raw//MiB)*MiB;return max(32*MiB,r)
def parts_total(b):return -(-b//part_bytes(b))
def deriv_est(b):return min(b*15//100,4*GiB)+128*MiB
def render_est(dticks,maxrate,audio):
    num=dticks*(maxrate+audio)*105;den=8*100*TPS;return -(-num//den)+64*MiB
def admit(used,res,limit,req):return used+res+req<=limit
Q={'part_bytes':[],'derivative_estimate':[],'render_estimate':[],'admit':[]}
for b in (1,100*MiB,GiB,16*GiB,17*GiB):Q['part_bytes'].append({'bytes':b,'part_bytes':part_bytes(b),'parts_total':parts_total(b)})
for b in (1*GiB,10*GiB,16*GiB,100*MiB):Q['derivative_estimate'].append({'bytes':b,'estimate':deriv_est(b)})
for d,mr,a in ((60*TPS,12_000_000,192_000),(3600*TPS,12_000_000,192_000),(1,6_000_000,192_000),(10800*TPS,48_000_000,192_000)):
    Q['render_estimate'].append({'duration_ticks':d,'maxrate_bps':mr,'audio_bps':a,'estimate':render_est(d,mr,a)})
for u,r,l,q in ((10,5,100,85),(10,5,100,86),(0,0,0,0),(0,0,0,1),(99*GiB,0,100*GiB,GiB),(99*GiB,0,100*GiB,GiB+1)):
    Q['admit'].append({'used':u,'reserved':r,'limit':l,'request':q,'ok':admit(u,r,l,q)})
Q['presets']={'h264_720p':[1280,720,5_000_000,6_000_000,12_000_000],'h264_1080p':[1920,1080,10_000_000,12_000_000,24_000_000],'h264_1440p':[2560,1440,18_000_000,21_600_000,43_200_000],'h264_2160p':[3840,2160,40_000_000,48_000_000,96_000_000]}
json.dump(Q,open('quota.json','w'),indent=1)
print(json.dumps(Q)[:900])
# ---- color
def s2l(c):return c/12.92 if c<=0.04045 else ((c+0.055)/1.055)**2.4
def l2s(c):
    c=min(max(c,0),1);return 12.92*c if c<=0.0031308 else 1.055*c**(1/2.4)-0.055
def exposure(rgb,ev):return [round(l2s(s2l(c)*2**ev),6) for c in rgb]
def cdl(rgb,slope=(1,1,1),offset=(0,0,0),power=(1,1,1),sat=1.0):
    o=[]
    for c,s,f,p in zip(rgb,slope,offset,power):
        v=max(c*s+f,0.0);v=v**p;o.append(v)
    y=0.2126*o[0]+0.7152*o[1]+0.0722*o[2]
    o=[y+sat*(v-y) for v in o]
    return [round(min(max(v,0),1),6) for v in o]
C={'srgb_to_linear':[{'in':v,'out':round(s2l(v),6)} for v in (0,0.04045,0.5,1)],
   'exposure':[{'rgb':[0.5,0.5,0.5],'ev':e,'out':exposure([0.5,0.5,0.5],e)} for e in (0,1,-1,5,-5)]+[{'rgb':[1,0.5,0],'ev':1,'out':exposure([1,0.5,0],1)}],
   'cdl':[
    {'rgb':[0.2,0.4,0.6],'slope':[1,1,1],'offset':[0,0,0],'power':[1,1,1],'sat':1.0,'out':cdl([0.2,0.4,0.6])},
    {'rgb':[0.2,0.4,0.6],'slope':[2,2,2],'offset':[0,0,0],'power':[1,1,1],'sat':1.0,'out':cdl([0.2,0.4,0.6],(2,2,2))},
    {'rgb':[0.2,0.4,0.6],'slope':[1,1,1],'offset':[-0.3,-0.3,-0.3],'power':[1,1,1],'sat':1.0,'out':cdl([0.2,0.4,0.6],offset=(-.3,-.3,-.3))},
    {'rgb':[0.2,0.4,0.6],'slope':[1,1,1],'offset':[0,0,0],'power':[2,2,2],'sat':1.0,'out':cdl([0.2,0.4,0.6],power=(2,2,2))},
    {'rgb':[0.2,0.4,0.6],'slope':[1,1,1],'offset':[0,0,0],'power':[1,1,1],'sat':0.0,'out':cdl([0.2,0.4,0.6],sat=0.0)},
    {'rgb':[0.2,0.4,0.6],'slope':[1,1,1],'offset':[0,0,0],'power':[1,1,1],'sat':2.0,'out':cdl([0.2,0.4,0.6],sat=2.0)},
    {'rgb':[0.9,0.9,0.9],'slope':[2,2,2],'offset':[0,0,0],'power':[1,1,1],'sat':1.0,'out':cdl([0.9,0.9,0.9],(2,2,2))}]}
# lut nodes
def lut_nodes(N,fn):
    return [fn(r/(N-1),g/(N-1),b/(N-1)) for b in range(N) for g in range(N) for r in range(N)]
def idx(N,r,g,b):return r+N*g+N*N*b
half=lambda r,g,b:cdl([r,g,b],(0.5,0.5,0.5))
nodes=lut_nodes(3,half)
C['lut']={'size':3,'transform':'cdl slope 0.5','index_formula':'r + N*g + N*N*b','samples':[{'r':r,'g':g,'b':b,'index':idx(3,r,g,b),'value':nodes[idx(3,r,g,b)]} for r,g,b in ((0,0,0),(1,0,0),(0,1,0),(0,0,1),(2,2,2),(2,1,0))]}
# deltaE76
def lab(rgb):
    r,g,b=[s2l(c) for c in rgb]
    X=0.4124564*r+0.3575761*g+0.1804375*b;Y=0.2126729*r+0.7151522*g+0.0721750*b;Z=0.0193339*r+0.1191920*g+0.9503041*b
    Xn,Yn,Zn=0.95047,1.0,1.08883
    f=lambda t:t**(1/3) if t>216/24389 else (24389/27*t+16)/116
    fx,fy,fz=f(X/Xn),f(Y/Yn),f(Z/Zn);return (116*fy-16,500*(fx-fy),200*(fy-fz))
def de76(a,b):
    la,lb=lab(a),lab(b);return math.sqrt(sum((x-y)**2 for x,y in zip(la,lb)))
C['delta_e76']=[{'a':a,'b':b,'de':round(de76(a,b),4)} for a,b in (([1,1,1],[1,1,1]),([1,1,1],[0,0,0]),([1,0,0],[0.98,0,0]),([0.5,0.5,0.5],[0.52,0.5,0.5]))]
C['lab']=[{'rgb':a,'lab':[round(x,3) for x in lab(a)]} for a in ([1,1,1],[0,0,0],[1,0,0],[0.5,0.5,0.5])]
from scipy.interpolate import PchipInterpolator
pts=[(0,0),(0.5,0.3),(1,1)];f=PchipInterpolator([p[0] for p in pts],[p[1] for p in pts],extrapolate=False)
C['pchip']={'points':pts,'eval':[{'x':x,'y':round(float(f(x)),6)} for x in (0,0.25,0.5,0.75,1)]}
json.dump(C,open('color.json','w'),indent=1)
print(json.dumps(C['cdl'][:3]));print(C['lut']['samples'][:3]);print(C['delta_e76']);print(C['pchip']);print(C['exposure'][:3])
# ---- canonical hash
import rfc8785,hashlib
doc={"b":1.0,"a":[3,2,1],"c":{"z":0.1,"y":"é","x":1e21,"w":-0.0,"v":100.5},"d":None,"e":True}
cb=rfc8785.dumps(doc);print(cb);print(hashlib.sha256(cb).hexdigest())
H={'input':doc,'canonical':cb.decode(),'sha256':hashlib.sha256(cb).hexdigest()}
doc2={"schema_version":1,"scale":1.0,"rotate":0.0,"position":[0,0],"name":"Привет","big":705600000}
cb2=rfc8785.dumps(doc2);H['cases']=[H.copy()] if False else []
H2=[{'input':doc,'canonical':cb.decode(),'sha256':hashlib.sha256(cb).hexdigest()},{'input':doc2,'canonical':cb2.decode(),'sha256':hashlib.sha256(cb2).hexdigest()}]
json.dump(H2,open('canonical.json','w'),indent=1,ensure_ascii=False);print(cb2)
