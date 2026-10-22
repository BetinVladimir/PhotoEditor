from verify import *
import ref_compile as C
SRC['hdrpq']={'path':'hdr_pq.mp4','has_audio':False,'duration':S(3),'hdr':'pq'}
SRC['hdrhlg']={'path':'hdr_hlg.mp4','has_audio':False,'duration':S(3),'hdr':'hlg'}
bad=0
def chk(name,out,t,x,y,exp,tol=4):
    global bad
    got=px.yuv(out,t-0.001,x,y); ok=got and all(abs(got[i]-exp[i])<=tol for i in range(3)); bad+= (not ok)
    print(('  ok ' if ok else '  BAD'),name,t,(x,y),got,tuple(round(v) for v in exp))
doc=mkdoc([vt('V1',[clip('c1','video','hdrpq',0,0,S(2)),clip('c2','video','hdrhlg',S(2),0,S(1))]),at_('A1',[])])
plan=render(doc,'g15.mov'); print('G15',dur_frames('g15.mov')); print([d for d in plan['decisions']])
chk('pq mid','g15.mov',1.0,160,90,(184,128,128))
got=px.yuv('g15.mov',2.5,160,90); print('hlg',got)
bad+=int(not (100<=got[0]<=235 and abs(got[1]-128)<=4 and abs(got[2]-128)<=4))
print('BAD',bad)
