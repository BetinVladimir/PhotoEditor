from verify import *
import ref_compile as C
def write_invert(name,size=17):
    with open(name,'w') as f:
        f.write(f'LUT_3D_SIZE {size}\n')
        for b in range(size):
            for g in range(size):
                for r in range(size):
                    f.write('%.6f %.6f %.6f\n'%(1-r/(size-1),1-g/(size-1),1-b/(size-1)))
bad=0
def chk(name,out,t,x,y,exp,tol=6):
    global bad
    got=px.yuv(out,t-0.001,x,y); ok=got and all(abs(got[i]-exp[i])<=tol for i in range(3)); bad+= (not ok)
    print(('  ok ' if ok else '  BAD'),name,t,(x,y),got,tuple(round(v) for v in exp))
eff=[{'id':'e1','type':'color.lut3d','enabled':True,'params':{'lut':{'kind':'lut','media_id':'L','sha256':'x'},'intensity':1.0}}]
ln=C.lut_name(eff); write_invert(ln); print(ln)
CY=rgb2yuv(0,255,255)
# клип с LUT (красный -> циан), второй клип без LUT на V2 поверх справа? нет: последовательно
doc=mkdoc([vt('V1',[clip('c1','video','red',0,0,S(2),effects=eff),clip('c2','video','red',S(2),0,S(2))]),at_('A1',[])])
render(doc,'g7a.mov'); chk('clip lut','g7a.mov',1.0,160,90,CY); chk('no lut','g7a.mov',3.0,160,90,rgb2yuv(255,0,0))
# мастер-LUT: зелёный -> пурпурный
doc=mkdoc([vt('V1',[clip('c1','video','green',0,0,S(2))]),at_('A1',[])],master={'effects':eff,'audio':{}})
render(doc,'g7b.mov'); chk('master lut','g7b.mov',1.0,160,90,rgb2yuv(255,0,255))
# и клип, и мастер -> двойная инверсия = исходный
doc=mkdoc([vt('V1',[clip('c1','video','green',0,0,S(2),effects=eff)]),at_('A1',[])],master={'effects':eff,'audio':{}})
render(doc,'g7c.mov'); chk('double','g7c.mov',1.0,160,90,rgb2yuv(0,255,0))
# фото-LUT и прозрачные полосы (4:3 blue на 16:9 с LUT): полосы не должны стать цветом LUT(чёрного)=белый
doc=mkdoc([vt('V1',[clip('c1','video','green',0,0,S(2))]),vt('V2',[clip('c2','video','blue43',0,0,S(2),effects=eff)]),at_('A1',[])])
render(doc,'g7d.mov'); chk('bars stay transparent','g7d.mov',1.0,10,90,rgb2yuv(0,255,0)); chk('lut content','g7d.mov',1.0,160,90,rgb2yuv(255,255,0))
print('BAD',bad)
