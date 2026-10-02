export type StreamEvent={cursor:string;type:string;payload:unknown};

export async function readEventStream(body:ReadableStream<Uint8Array>,onEvent:(event:StreamEvent)=>void,signal:AbortSignal){
 const reader=body.getReader();const decoder=new TextDecoder();let buffer='',data:string[]=[],id='';
 const abort=()=>{void reader.cancel().catch(()=>undefined)};signal.addEventListener('abort',abort,{once:true});
 const line=(value:string)=>{
  if(value===''){
   if(data.length){
    const event=JSON.parse(data.join('\n')) as StreamEvent;
    if(!/^(0|[1-9][0-9]{0,15})$/.test(id)||event.cursor!==id||typeof event.type!=='string'||event.type.length>256)throw new Error('Invalid SSE event');
    onEvent(event);
   }
   data=[];id='';return;
  }
  if(value.startsWith(':'))return;
  const colon=value.indexOf(':');const field=colon<0?value:value.slice(0,colon);
  let content=colon<0?'':value.slice(colon+1);if(content.startsWith(' '))content=content.slice(1);
  if(field==='data')data.push(content);if(field==='id')id=content;
 };
 try{
  while(true){
   signal.throwIfAborted();const {done,value}=await reader.read();signal.throwIfAborted();if(done)break;
   buffer+=decoder.decode(value,{stream:true});
   if(buffer.length+data.join('').length>1048576)throw new Error('SSE frame too large');
   let index:number;
   while((index=buffer.indexOf('\n'))>=0){let value=buffer.slice(0,index);buffer=buffer.slice(index+1);if(value.endsWith('\r'))value=value.slice(0,-1);line(value)}
  }
 }finally{signal.removeEventListener('abort',abort);await reader.cancel().catch(()=>undefined);reader.releaseLock()}
}
