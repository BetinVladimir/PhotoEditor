import {expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({size:22,modified:1,closed:0,short:false}));
vi.mock('react-native',()=>({Platform:{OS:'ios'}}));
vi.mock('expo-document-picker',()=>({getDocumentAsync:vi.fn()}));
vi.mock('./interchange-archive-screen',()=>({pickWebArchive:vi.fn()}));
vi.mock('expo-file-system',()=>({File:class{
 exists=true;get size(){return state.size;}get lastModified(){return state.modified;}
 open(){return {offset:0,readBytes:(length:number)=>new Uint8Array(state.short?length-1:length),close:()=>{state.closed++;}};}
}}));
import {nativeArchiveSource} from './interchange-archive-picker';
it('reads bounded native bytes and closes handles; detects changed metadata',async()=>{
 state.size=22;state.modified=1;state.closed=0;state.short=false;
 const signal=new AbortController().signal,source=await nativeArchiveSource('file:///archive.zip','archive.zip',22,signal);
 expect(source.sha256).toMatch(/^[a-f0-9]{64}$/);expect(state.closed).toBe(1);expect((await source.read(0,22,signal)).length).toBe(22);expect(state.closed).toBe(2);
 state.modified=2;await expect(source.read(0,22,signal)).rejects.toThrow('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');expect(state.closed).toBe(2);
});
it('rejects truncated native reads and cancelled preparation',async()=>{
 state.size=22;state.modified=1;state.short=true;state.closed=0;
 await expect(nativeArchiveSource('file:///archive.zip','archive.zip',22,new AbortController().signal)).rejects.toThrow('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');expect(state.closed).toBe(1);
 const c=new AbortController();c.abort();await expect(nativeArchiveSource('file:///archive.zip','archive.zip',22,c.signal)).rejects.toBeDefined();state.short=false;
});
