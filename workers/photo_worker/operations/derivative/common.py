from contextlib import ExitStack
import numpy as np
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array, read_alpha_array


def validate_image(ctx,artifacts,ref):
    image=artifacts.contract(ref,WorkingImage)
    if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or image.canvas.width*image.canvas.height>200000000:raise ValueError('Invalid derivative image')
    required=[ref,image.pixels,*([image.alpha] if image.alpha else [])]
    return image,required


def open_image(stack:ExitStack,ctx,artifacts,image):
    local=stack.enter_context(artifacts.input(image.pixels,suffix='.npy'));rgb=stack.enter_context(read_array(local.path,(image.canvas.height,image.canvas.width,3),'float32',ctx.check_active,unit_interval=True));alpha=None
    if image.alpha is not None:
        local=stack.enter_context(artifacts.input(image.alpha,suffix='.npy'));raw=stack.enter_context(read_alpha_array(local.path,(image.canvas.height,image.canvas.width),ctx.check_active));alpha=np.asarray(raw,np.float32)/65535 if raw.dtype==np.uint16 else np.asarray(raw,np.float32)
    return rgb,alpha


def stage_arrays(artifacts,reservation,rgb,alpha):
    h,w=rgb.shape[:2];path=reservation.path('derivative-rgb','.npy');mm=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w,3));mm[:]=rgb;mm.flush();mm._mmap.close();pixel=artifacts.stage(path,reservation,'working_rgb','application/octet-stream');outputs=[pixel];written=path.stat().st_size;alpha_ref=None
    if alpha is not None:
        apath=reservation.path('derivative-alpha','.npy');am=np.lib.format.open_memmap(apath,mode='w+',dtype='<f4',shape=(h,w));am[:]=np.clip(alpha,0,1);am.flush();am._mmap.close();alpha_ref=artifacts.stage(apath,reservation,'working_alpha','application/octet-stream');outputs.append(alpha_ref);written+=apath.stat().st_size
    return pixel,alpha_ref,outputs,written
