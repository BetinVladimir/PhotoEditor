"""Reference fixture generator for docs/video (verified 2026-10-22 with ffmpeg 6/7). Usage: python make_fixtures_ref.py OUTDIR"""
import subprocess, sys, json
from pathlib import Path
def run(args):
    r = subprocess.run(args, capture_output=True, text=True)
    if r.returncode != 0:
        print(r.stderr[-2000:]); sys.exit(2)
    return r.stdout
def frames(path):
    out = run(['ffprobe','-v','error','-count_frames','-select_streams','v:0','-show_entries','stream=nb_read_frames','-of','default=nw=1:nk=1',str(path)])
    return int(out.strip())
def make_hard_cuts(o):
    run(['ffmpeg','-v','error','-y','-f','lavfi','-i','testsrc2=size=320x180:rate=25','-f','lavfi','-i','mandelbrot=size=320x180:rate=25','-f','lavfi','-i','smptebars=size=320x180:rate=25',
         '-filter_complex','[0:v]trim=end_frame=50,setpts=PTS-STARTPTS[a];[1:v]trim=end_frame=50,setpts=PTS-STARTPTS[b];[2:v]trim=end_frame=50,setpts=PTS-STARTPTS[c];[a][b][c]concat=n=3:v=1:a=0[v]',
         '-map','[v]','-pix_fmt','yuv420p','-c:v','libx264','-g','25','-crf','18',str(o/'hard_cuts.mp4')])
def make_dissolve(o):
    run(['ffmpeg','-v','error','-y','-f','lavfi','-i','testsrc2=size=320x180:rate=25:duration=3','-f','lavfi','-i','mandelbrot=size=320x180:rate=25',
         '-filter_complex','[0:v]trim=end_frame=55,setpts=PTS-STARTPTS[a];[1:v]trim=end_frame=55,setpts=PTS-STARTPTS[b];[a][b]xfade=transition=fade:duration=0.4:offset=1.8[v]',
         '-map','[v]','-pix_fmt','yuv420p','-c:v','libx264','-g','25','-crf','18',str(o/'dissolve.mp4')])
def make_fades(o):
    run(['ffmpeg','-v','error','-y','-f','lavfi','-i','testsrc2=size=320x180:rate=25:duration=2','-f','lavfi','-i','mandelbrot=size=320x180:rate=25',
         '-filter_complex','[0:v]trim=end_frame=50,setpts=PTS-STARTPTS,fade=t=out:st=1.4:d=0.6[a];[1:v]trim=end_frame=50,setpts=PTS-STARTPTS,fade=t=in:st=0:d=0.6[b];[a][b]concat=n=2:v=1:a=0[v]',
         '-map','[v]','-pix_fmt','yuv420p','-c:v','libx264','-g','25','-crf','18',str(o/'fades.mp4')])
def make_src(o,name,video,freq):
    run(['ffmpeg','-v','error','-y','-f','lavfi','-i',f'{video}=size=320x180:rate=25:duration=10','-f','lavfi','-i',f'sine=frequency={freq}:sample_rate=48000:duration=10',
         '-c:v','libx264','-g','25','-pix_fmt','yuv420p','-c:a','aac',str(o/name)])
def make_sine20(o):
    run(['ffmpeg','-v','error','-y','-f','lavfi','-i','sine=frequency=1000:sample_rate=48000:duration=20,volume=-20dB','-ac','2',str(o/'sine20.wav')])
def make_g1(o):
    run(['ffmpeg','-v','error','-y','-i',str(o/'srcA.mp4'),'-i',str(o/'srcB.mp4'),'-filter_complex',
     '[0:v]trim=start=2:end=5,setpts=PTS-STARTPTS,fps=25,scale=320:180:force_original_aspect_ratio=decrease,pad=320:180:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p[v0];'
     '[1:v]trim=start=1:end=3,setpts=PTS-STARTPTS,fps=25,scale=320:180:force_original_aspect_ratio=decrease,pad=320:180:(ow-iw)/2:(oh-ih)/2,setsar=1,format=yuv420p[v1];'
     '[v0][v1]concat=n=2:v=1:a=0[vout];'
     '[0:a]atrim=start=2:end=5,asetpts=PTS-STARTPTS,aresample=48000,volume=-6dB,afade=t=in:st=0:d=0.5[a0];'
     '[1:a]atrim=start=1:end=3,asetpts=PTS-STARTPTS,aresample=48000,volume=-6dB[a1];[a0][a1]concat=n=2:v=0:a=1[aout]',
     '-map','[vout]','-map','[aout]','-c:v','libx264','-preset','veryfast','-pix_fmt','yuv420p','-b:v','1M','-maxrate','1.2M','-bufsize','2.4M','-c:a','aac','-b:a','192k','-movflags','+faststart','-map_metadata','-1',str(o/'g1.mp4')])
def make_g2(o):
    run(['ffmpeg','-v','error','-y','-i',str(o/'srcA.mp4'),'-i',str(o/'srcB.mp4'),'-filter_complex',
     '[0:v]trim=start=0:end=3.5,setpts=PTS-STARTPTS,fps=25,format=yuv420p[v0];[1:v]trim=start=4.5:end=8,setpts=PTS-STARTPTS,fps=25,format=yuv420p[v1];'
     '[v0][v1]xfade=transition=fade:duration=1:offset=2.5[vout];'
     '[0:a]atrim=start=0:end=3.5,asetpts=PTS-STARTPTS,aresample=48000[a0];[1:a]atrim=start=4.5:end=8,asetpts=PTS-STARTPTS,aresample=48000[a1];[a0][a1]acrossfade=d=1:c1=tri:c2=tri[aout]',
     '-map','[vout]','-map','[aout]','-c:v','libx264','-preset','veryfast','-pix_fmt','yuv420p','-c:a','aac','-b:a','192k',str(o/'g2.mp4')])
def duration(p):
    return float(run(['ffprobe','-v','error','-show_entries','format=duration','-of','default=nw=1:nk=1',str(p)]).strip())
EXPECT_FRAMES={'hard_cuts.mp4':150,'dissolve.mp4':100,'fades.mp4':100,'srcA.mp4':250,'srcB.mp4':250,'g1.mp4':125,'g2.mp4':150}
def main(out):
    o=Path(out);o.mkdir(parents=True,exist_ok=True)
    make_hard_cuts(o);make_dissolve(o);make_fades(o)
    make_src(o,'srcA.mp4','testsrc2',440);make_src(o,'srcB.mp4','testsrc',880);make_sine20(o)
    make_g1(o);make_g2(o)
    ok=True
    for n,e in EXPECT_FRAMES.items():
        f=frames(o/n);print(n,f,'OK' if f==e else f'FAIL expected {e}');ok&=f==e
    for n,e in (('g1.mp4',5.0),('g2.mp4',6.0)):
        d=duration(o/n);good=abs(d-e)<0.02;print(n,'duration',round(d,3),'OK' if good else 'FAIL');ok&=good
    sys.exit(0 if ok else 2)
if __name__=='__main__': main(sys.argv[1])
