import subprocess
def yuv(path, t, x=None, y=None, w=320, h=180):
    """YUV444 limited пикселя (центр или x,y) кадра в момент t (кадр с pts<=t+eps)."""
    vf = "format=yuv444p"
    if x is None:
        x, y = w // 2, h // 2
    vf += f",crop=1:1:{x}:{y}"
    r = subprocess.run(["ffmpeg","-hide_banner","-loglevel","error","-ss",f"{t:.6f}","-i",path,"-frames:v","1","-vf",vf,"-f","rawvideo","-"],capture_output=True)
    d = r.stdout
    return tuple(d[:3]) if len(d)>=3 else None
def probe(path):
    r = subprocess.run(["ffprobe","-v","error","-count_frames","-select_streams","v:0","-show_entries","stream=nb_read_frames,duration","-show_entries","format=duration","-of","default=nw=1",path],capture_output=True,text=True)
    return r.stdout.split()
