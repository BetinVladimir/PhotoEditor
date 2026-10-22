from verify import *
def near(a,b,tol=10): return a is not None and all(abs(a[i]-b[i])<=tol for i in range(3))
G=rgb2yuv(*COL['green']); B=rgb2yuv(*COL['blue43']); R=rgb2yuv(*COL['red'])
bad=0
def chk(name,out,t,x,y,exp,tol=10):
    global bad
    got=px.yuv(out,t-0.001,x,y); ok=near(got,exp,tol); bad+= (not ok)
    print(('  ok ' if ok else '  BAD'),name,t,(x,y),got,tuple(round(v) for v in exp))
# T4a: V1 зелёный, V2 blue43 (4:3) fit -> полосы прозрачные
doc=mkdoc([vt('V1',[clip('c1','video','green',0,0,S(3))]),vt('V2',[clip('c2','video','blue43',0,0,S(3))]),at_('A1',[])])
render(doc,'g4a.mov')
chk('bars transparent',  'g4a.mov',1.0, 10,90,G); chk('content','g4a.mov',1.0,160,90,B); chk('bar-right','g4a.mov',1.0,310,90,G)
chk('edge content','g4a.mov',1.0,50,90,B); chk('edge bar','g4a.mov',1.0,30,90,G)
# T4b: transform scale .5, position (.25,-.25) на фото orange 16:9
doc=mkdoc([vt('V1',[clip('c1','video','green',0,0,S(3))]),
           vt('V2',[clip('p1','photo','orange',0,duration=S(3),transform={'scale':0.5,'position':[0.25,-0.25],'rotate':0.0,'crop':[0,0,0,0]})]),at_('A1',[])])
O=rgb2yuv(*COL['orange']); render(doc,'g4b.mov')
chk('pip center','g4b.mov',1.0,240,45,O); chk('pip outside-left','g4b.mov',1.0,150,45,G); chk('pip inside-left','g4b.mov',1.0,185,45,O)
chk('pip below','g4b.mov',1.0,240,100,G); chk('pip right-inside','g4b.mov',1.0,315,45,O); chk('canvas below-left','g4b.mov',1.0,80,135,G)
# T4c: rotate 90 на 4:3 blue: фигура 120x160 (после fit 240x180 -> повёрнута на 90)
doc=mkdoc([vt('V1',[clip('c1','video','green',0,0,S(3))]),
           vt('V2',[clip('c2','video','blue43',0,0,S(3),transform={'scale':1.0,'position':[0,0],'rotate':90.0,'crop':[0,0,0,0]})]),at_('A1',[])])
render(doc,'g4c.mov')
# после поворота на 90°: размер 180x240 вокруг центра (160,90): x 70..250, y -30..210 (за кадром по y)
chk('rot inside','g4c.mov',1.0,100,10,B); chk('rot outside-x','g4c.mov',1.0,50,90,G); chk('rot outside-x2','g4c.mov',1.0,270,90,G)
# T4d: crop [0.25,0,0.25,0] на blue43 (после fit x 40..280): видно x 80..240
doc=mkdoc([vt('V1',[clip('c1','video','green',0,0,S(3))]),
           vt('V2',[clip('c2','video','blue43',0,0,S(3),transform={'scale':1.0,'position':[0,0],'rotate':0.0,'crop':[0.25,0,0.25,0]})]),at_('A1',[])])
render(doc,'g4d.mov')
chk('crop gone-left','g4d.mov',1.0,60,90,G); chk('crop visible','g4d.mov',1.0,160,90,B); chk('crop gone-right','g4d.mov',1.0,260,90,G)
chk('crop edge-in','g4d.mov',1.0,90,90,B); chk('crop edge-out','g4d.mov',1.0,70,90,G)
# T4e: fit=fill для blue43 -> заполняет весь кадр; fit=none -> 160x120 по центру
doc=mkdoc([vt('V1',[clip('c1','video','green',0,0,S(3))]),vt('V2',[clip('c2','video','blue43',0,0,S(3),fit='fill')]),at_('A1',[])]); render(doc,'g4e.mov')
chk('fill corner','g4e.mov',1.0,5,5,B); chk('fill center','g4e.mov',1.0,160,90,B)
doc=mkdoc([vt('V1',[clip('c1','video','green',0,0,S(3))]),vt('V2',[clip('c2','video','blue43',0,0,S(3),fit='none')]),at_('A1',[])]); render(doc,'g4f.mov')
chk('none inside','g4f.mov',1.0,100,90,B); chk('none outside','g4f.mov',1.0,70,90,G); chk('none outside-y','g4f.mov',1.0,160,20,G)
# T4g: прозрачность слоя при кроссфейде на верхней дорожке: V2 два клипа
doc=mkdoc([vt('V1',[clip('c1','video','green',0,0,S(4))]),
           vt('V2',[clip('r1','video','red',0,0,S(2)),clip('b1','video','blue43',S(2),0,S(2),transition_in={'kind':'crossfade','duration':S(1),'align':'center'})]),at_('A1',[])])
render(doc,'g4g.mov'); bad+=check_video(doc,'g4g.mov',[1.0,1.5,2.0,2.5,3.0])  # blue43 с барами: центр всё равно синий
print('BAD',bad)
