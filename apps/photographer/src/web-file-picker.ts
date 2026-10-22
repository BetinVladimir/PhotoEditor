/** Keep the chooser's input live until selection/cancellation settles. */
export function pickWebFiles(signal?:AbortSignal):Promise<File[]>{
 return new Promise((resolve,reject)=>{
  if(signal?.aborted){reject(signal.reason??new DOMException('Aborted','AbortError'));return;}
  const input=document.createElement('input');input.type='file';input.multiple=true;
  input.accept='.jpg,.jpeg,.png,.tif,.tiff,.dng,.cr2,.nef,.arw,.raf,.orf,.rw2';input.hidden=true;
  let settled=false;
  const cleanup=()=>{input.onchange=null;input.oncancel=null;signal?.removeEventListener('abort',abort);input.remove();};
  const finish=(files:File[])=>{if(settled)return;settled=true;cleanup();resolve(files);};
  const abort=()=>{if(settled)return;settled=true;cleanup();reject(signal?.reason??new DOMException('Aborted','AbortError'));};
  input.onchange=()=>finish(Array.from(input.files??[]));input.oncancel=()=>finish([]);
  signal?.addEventListener('abort',abort,{once:true});document.body.appendChild(input);
  try{input.click();}catch(error){settled=true;cleanup();reject(error);}
 });
}
