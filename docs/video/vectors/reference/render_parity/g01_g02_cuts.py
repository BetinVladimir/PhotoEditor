from verify import *
# G1: два клипа, жёсткий склей, -6 дБ, fade-in 0.5 у первого
doc=mkdoc([vt('V1',[clip('c1','video','red',0,S(2),S(5),audio={'gain_db':-6.0,'fade_in':S(0.5),'fade_out':0,'mute':False}),
                     clip('c2','video','green',S(3),S(1),S(3),audio={'gain_db':-6.0,'fade_in':0,'fade_out':0,'mute':False})]),at_('A1',[])])
plan=render(doc,'g1.mov'); print(dur_frames('g1.mov'))
bad=check_video(doc,'g1.mov',[0.0,1.0,2.9,3.0,4.0,4.96])+check_audio(doc,'g1.mov',[0.0,0.3,1.0,3.5])
# G2: кроссфейд 1 с, центр
doc2=mkdoc([vt('V1',[clip('c1','video','red',0,0,S(3)),clip('c2','video','green',S(3),S(5),S(8),transition_in={'kind':'crossfade','duration':S(1),'align':'center'})]),at_('A1',[])])
plan=render(doc2,'g2.mov'); print(dur_frames('g2.mov'))
bad+=check_video(doc2,'g2.mov',[0.0,2.0,2.5,2.7,3.0,3.3,3.5,4.0,5.9])+check_audio(doc2,'g2.mov',[1.0,2.45,2.95,3.45,4.0])
print('BAD',bad)
