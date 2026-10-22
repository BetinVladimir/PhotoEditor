from verify import *
import subprocess, struct, math
bad=0
def chk(name,out,t,x,y,exp,tol=10):
    global bad
    got=px.yuv(out,t-0.001,x,y); ok=got and all(abs(got[i]-exp[i])<=tol for i in range(3)); bad+= (not ok)
    print(('  ok ' if ok else '  BAD'),name,t,(x,y),got,tuple(round(v) for v in exp))
# T5a: Ken Burns на градиенте 16:9: scale 1 -> 2, центр (.5,.5) -> (.75,.5), ease_in_out, 2 с
mot={'kind':'pan_zoom','from':{'scale':1.0,'center':[0.5,0.5]},'to':{'scale':2.0,'center':[0.75,0.5]},'easing':'ease_in_out'}
doc=mkdoc([vt('V1',[clip('p1','photo','grad',0,duration=S(2),motion=mot)]),at_('A1',[])])
render(doc,'g5a.mov'); print('G5a',dur_frames('g5a.mov'))
_r=subprocess.run(["ffmpeg","-hide_banner","-loglevel","error","-i","photo_grad.png","-vf","crop=640:1:0:180,scale=out_color_matrix=bt709:out_range=tv,format=yuv444p","-f","rawvideo","-"],capture_output=True)
GY=list(_r.stdout[:640])
def grad_Y(xi):
    i=min(639,max(0,int(xi))); return GY[i]
for t in [0.0,0.5,1.0,1.5,1.96]:
    ph=K.photo_transform_at(doc['tracks'][0]['clips'][0],S(t)); z=ph['scale']; cx=ph['center'][0]
    x0=max(0,min(1-1/z,cx-1/(2*z)))   # доля ширины
    for ox in (80,160,240):
        xi=(x0+(ox/320.0)/z)*640
        chk('kb',  'g5a.mov',t,ox,90,(grad_Y(xi),128,128),tol=9)
# T5b: фото -> видео кроссфейд 1 с по центру (у фото бесконечные запасы, у видео src_in=2)
doc=mkdoc([vt('V1',[clip('p1','photo','orange',0,duration=S(3)),clip('c1','video','green',S(3),S(2),S(5),transition_in={'kind':'crossfade','duration':S(1),'align':'center'})]),at_('A1',[])])
render(doc,'g5b.mov'); print('G5b',dur_frames('g5b.mov')); bad+=check_video(doc,'g5b.mov',[1.0,2.52,3.0,3.28,3.6,4.5])
# T5c: две дорожки, верхняя — фото с fade_in
doc=mkdoc([vt('V1',[clip('c1','video','green',0,0,S(4))]),vt('V2',[clip('p1','photo','orange',S(1),duration=S(2),transition_in={'kind':'fade_in','duration':S(1),'align':'center'})]),at_('A1',[])])
render(doc,'g5c.mov'); bad+=check_video(doc,'g5c.mov',[0.5,1.0,1.5,2.0,2.5,3.2])
# T6: скорость
def freq(path,t0,t1):
    r=subprocess.run(["ffmpeg","-hide_banner","-loglevel","error","-i",path,"-vn","-f","s16le","-ac","1","-ar","48000","-"],capture_output=True)
    s=struct.unpack('<%dh'%(len(r.stdout)//2),r.stdout)[int(t0*48000):int(t1*48000)]
    z=sum(1 for i in range(1,len(s)) if s[i-1]<0<=s[i]); return z/(t1-t0)
doc=mkdoc([vt('V1',[clip('c1','video','red',0,0,S(4),speed={'num':2,'den':1}),clip('c2','video','green',S(2),0,S(1),speed={'num':1,'den':4})]),at_('A1',[])])
render(doc,'g6.mov'); print('G6',dur_frames('g6.mov'),'end',K.doc_end(doc)/K.TPS)
print('  freq fast (440 ожидается)',freq('g6.mov',0.5,1.5),' slow (880 ожидается)',freq('g6.mov',2.5,5.5))
bad+=check_video(doc,'g6.mov',[0.5,1.9,2.1,5.0])
print('BAD',bad)
