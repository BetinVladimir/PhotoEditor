from verify import *
import struct, math, subprocess
import ref_compile as C
def tone_rms(path,f,t0,t1):
    r=subprocess.run(["ffmpeg","-hide_banner","-loglevel","error","-i",path,"-vn","-f","s16le","-ac","1","-ar","48000","-"],capture_output=True)
    s=struct.unpack('<%dh'%(len(r.stdout)//2),r.stdout)[int(t0*48000):int(t1*48000)]
    n=len(s); c=sum(x/32768*math.cos(2*math.pi*f*i/48000) for i,x in enumerate(s))*2/n; q=sum(x/32768*math.sin(2*math.pi*f*i/48000) for i,x in enumerate(s))*2/n
    return math.hypot(c,q)/math.sqrt(2)
duck={'id':'dk','type':'audio.duck','enabled':True,'params':{'sidechain_tracks':['A2'],'threshold_db':-30,'depth_db':12,'attack_ms':20,'release_ms':250}}
# музыка (green 880) на A1 с gain -6 дБ дорожки; речь (blue 220) на A2 с 2 по 5 с
doc=mkdoc([vt('V1',[clip('v1','video','red',0,0,S(8))]),
           at_('A1',[clip('mus','audio','green',0,0,S(8),effects=[duck],audio={'gain_db':0.0,'fade_in':0,'fade_out':0,'mute':False})],gain_db=-6.0),
           at_('A2',[clip('sp','audio','blue43',S(2),0,S(3))])])
plan=render(doc,'g8.mov'); print('G8',dur_frames('g8.mov'))
base=tone_rms('g8.mov',880,0.2,1.8); duck_lvl=tone_rms('g8.mov',880,3.0,4.8); after=tone_rms('g8.mov',880,7.0,7.9)
print('  музыка без речи %.4f (ожид %.4f)  под речью %.4f  после %.4f'%(base,0.125*10**(-6/20)/math.sqrt(2),duck_lvl,after))
print('  ослабление под речью, дБ: %.2f'%(20*math.log10(base/duck_lvl)))
sp=tone_rms('g8.mov',220,3.0,4.8); print('  речь %.4f (ожид %.4f)'%(sp,0.125/math.sqrt(2)))
# ожидание по формуле: уровень SC (RMS stereo) -21.07 dBFS; threshold -30; ratio 20 -> GR=(L-T)*(1-1/R)
Lsc=20*math.log10(0.125/math.sqrt(2)); print('  формула GR = %.2f дБ'%((Lsc-(-30))*(1-1/20)))
print(plan['filter_complex'][-900:])
# mute / hidden
doc=mkdoc([vt('V1',[clip('v1','video','red',0,0,S(3))],visible=False),at_('A1',[clip('m','audio','green',0,0,S(3))],muted=True),at_('A2',[clip('n','audio','blue43',0,0,S(3),audio={'gain_db':-12.0,'fade_in':S(1),'fade_out':S(1),'mute':False})])])
render(doc,'g9.mov'); print('G9',dur_frames('g9.mov')); b=check_audio(doc,'g9.mov',[0.2,0.8,1.5,2.5,2.9]); print('  hidden track pixel',px.yuv('g9.mov',1.0,160,90))
print('  tones: green(880)=%.4f blue(220)=%.4f'%(tone_rms('g9.mov',880,0.5,2.5),tone_rms('g9.mov',220,1.2,1.8)))
att = 20*math.log10(base/duck_lvl)
badc = int(not (8.0 <= att <= 13.0)) + int(abs(base-0.125*10**(-6/20)/math.sqrt(2)) > 0.002) + int(abs(after-base) > 0.002) + int(abs(sp-0.125/math.sqrt(2)) > 0.003)
badc += int(abs(tone_rms('g9.mov',880,0.5,2.5)) > 0.001) + int(abs(px.yuv('g9.mov',1.0,160,90)[0]-16) > 1)
print('BAD', badc)
