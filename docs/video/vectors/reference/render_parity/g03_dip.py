from verify import *
import ref_compile as C
bad=0
# T3: dip_to_black + fade_in
doc=mkdoc([vt('V1',[clip('c1','video','red',0,S(1),S(4)),
                    clip('c2','video','green',S(3),S(2),S(5),transition_in={'kind':'dip_to_black','duration':S(1),'align':'center'}),
                    clip('c3','video','red',S(7),S(1),S(3),transition_in={'kind':'fade_in','duration':S(1),'align':'center'})]),at_('A1',[])])
render(doc,'g3.mov'); print('G3',dur_frames('g3.mov'))
bad+=check_video(doc,'g3.mov',[0.0,2.0,2.6,2.8,3.0,3.2,3.4,4.0,6.5,7.0,7.5,8.0,8.9])
bad+=check_audio(doc,'g3.mov',[1.0,2.55,2.95,3.45,7.5])
print('BAD',bad)
