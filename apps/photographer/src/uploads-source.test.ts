import {beforeEach,expect,it,vi} from 'vitest';
vi.mock('@react-native-async-storage/async-storage',()=>({default:{}}));
const native=vi.hoisted(()=>({bytes:new Uint8Array([1,2,3]),exists:true,opens:0,closes:0,fail:false,short:false}));
vi.mock('expo-file-system',()=>({File:class {
 get exists(){return native.exists}get size(){return native.bytes.length}
 open(){native.opens++;return{offset:0,readBytes(length:number){if(native.fail)throw new Error('read failed');return native.bytes.slice(this.offset,this.offset+length-(native.short?1:0))},close(){native.closes++}}}
}}));
beforeEach(()=>{native.bytes=new Uint8Array([1,2,3]);native.exists=true;native.opens=0;native.closes=0;native.fail=false;native.short=false});
import {OwnerMultipartAPI,nativeSource,webSource} from './uploads';
it('rejects an already cancelled file before reading',async()=>{
 const c=new AbortController();c.abort();const slice=vi.fn();
 await expect(webSource({name:'x.jpg',size:3,lastModified:1,slice} as unknown as File,undefined,c.signal)).rejects.toThrow();expect(slice).not.toHaveBeenCalled();
});
it('discards a late initial browser read after cancellation',async()=>{
 const c=new AbortController();let release!:(buffer:ArrayBuffer)=>void,entered!:()=>void;
 const started=new Promise<void>(r=>{entered=r}),buffer=new Promise<ArrayBuffer>(r=>{release=r});
 const file={name:'x.jpg',size:3,lastModified:1,slice:()=>({arrayBuffer:()=>{entered();return buffer}})} as unknown as File;
 const pending=webSource(file,undefined,c.signal);const rejected=expect(pending).rejects.toThrow();await started;c.abort();release(new ArrayBuffer(3));await rejected;
});

it('rechecks native file digest and closes every handle after reads',async()=>{
 const source=await nativeSource('file:///cache/frame.jpg','frame.jpg',3);const original=source.sha256;
 expect(await source.verify!(new AbortController().signal)).toBe(original);native.bytes=new Uint8Array([3,2,1]);expect(await source.verify!(new AbortController().signal)).not.toBe(original);expect(native.opens).toBe(3);expect(native.closes).toBe(native.opens);
});
it.each(['throw','short'])('closes native handle on %s read',async mode=>{
 const source=await nativeSource('file:///cache/frame.jpg','frame.jpg',3);native.fail=mode==='throw';native.short=mode==='short';await expect(source.read(0,3,new AbortController().signal)).rejects.toThrow();expect(native.closes).toBe(native.opens);
});
it('refuses changed native size before opening a handle',async()=>{
 const source=await nativeSource('file:///cache/frame.jpg','frame.jpg',3);const opens=native.opens;native.bytes=new Uint8Array(4);await expect(source.read(0,3,new AbortController().signal)).rejects.toThrow('UPLOAD_FILE_UNAVAILABLE');expect(native.opens).toBe(opens);
});
it.each([[-1,2],[0,4],[0,0],[0.5,2],[0,Infinity]])('refuses invalid native read range %s..%s before opening',async(start,end)=>{
 const source=await nativeSource('file:///cache/frame.jpg','frame.jpg',3);const opens=native.opens;await expect(source.read(start,end,new AbortController().signal)).rejects.toThrow('UPLOAD_READ_RANGE_INVALID');expect(native.opens).toBe(opens);
});

it('does not open a native handle when already cancelled',async()=>{
 const c=new AbortController();c.abort();await expect(nativeSource('file:///cache/frame.jpg','frame.jpg',3,0,undefined,c.signal)).rejects.toThrow();expect(native.opens).toBe(0);
});


it('binds Begin and status to server-normalized filename without changing durable local metadata',async()=>{
 const id='11111111-1111-4111-8111-111111111111',project='22222222-2222-4222-8222-222222222222';
 const request=vi.fn().mockResolvedValue({id,project_id:project,filename:'frame.jpg',expected_size:3,part_bytes:16777216,parts_total:1,state:'uploading'});
 const client={request};const api=new OwnerMultipartAPI(client as unknown as ConstructorParameters<typeof OwnerMultipartAPI>[0],'tenant');
 const record={localId:'stable-key',tenant:'tenant',projectId:project,uploadId:id,name:'\u0085 frame.jpg \u0085',size:3,lastModified:0,sha256:'a'.repeat(64),parts:[],state:'paused' as const};
 const signal=new AbortController().signal;
 expect((await api.begin(record,signal)).id).toBe(id);
 expect(request.mock.calls[0][1].body.filename).toBe('frame.jpg');
 expect(request.mock.calls[0][1].body.format).toBe('jpg');
 expect(request.mock.calls[0][1].idempotencyKey).toBe('stable-key');
 expect((await api.status(record,signal)).id).toBe(id);
 expect(record.name).toBe('\u0085 frame.jpg \u0085');
});
