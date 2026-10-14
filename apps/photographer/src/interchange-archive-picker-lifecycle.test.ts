import {beforeEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({files:new Set<string>(),short:false,failRelease:false}));
vi.mock('react-native',()=>({Platform:{OS:'ios'}}));
vi.mock('expo-document-picker',()=>({getDocumentAsync:vi.fn()}));
vi.mock('./interchange-archive-screen',()=>({pickWebArchive:vi.fn()}));
vi.mock('expo-file-system',()=>({Paths:{cache:{uri:'file:///cache/'}},File:class{
 constructor(public uri:string){}
 get exists(){return state.files.has(this.uri);}size=22;lastModified=1;
 delete(){state.files.delete(this.uri);}
 open(){return {offset:0,readBytes:(n:number)=>new Uint8Array(state.short?n-1:n),close:vi.fn()};}
}}));
vi.mock('./interchange-archive-cache',()=>({sweepArchiveCache:vi.fn(),stageArchiveCache:vi.fn()}));
import {File} from 'expo-file-system';
import {getDocumentAsync} from 'expo-document-picker';
import {stageArchiveCache,sweepArchiveCache} from './interchange-archive-cache';
import {pickInterchangeArchive} from './interchange-archive-picker';
import {releaseArchiveSource} from './interchange-archive-source-lifecycle';
const pickerURI='file:///cache/picker.zip',stagedURI='file:///cache/photoeditor-interchange-v1/staged.zip';
const release=vi.fn(()=>{if(state.failRelease)throw new Error('delete failed');state.files.delete(stagedURI);});
beforeEach(()=>{
 vi.clearAllMocks();state.files.clear();state.short=false;state.failRelease=false;
 state.files.add(pickerURI);
 vi.mocked(getDocumentAsync).mockResolvedValue({canceled:false,assets:[{uri:pickerURI,name:'archive.zip',size:22,lastModified:1,mimeType:'application/zip'}]});
 vi.mocked(stageArchiveCache).mockImplementation(()=>{state.files.delete(pickerURI);state.files.add(stagedURI);return {file:new File(stagedURI),release};});
});
it('sweeps before picking, hashes the staged copy and releases it explicitly',async()=>{
 const source=await pickInterchangeArchive(new AbortController().signal,vi.fn());
 expect(sweepArchiveCache).toHaveBeenCalledOnce();expect(vi.mocked(sweepArchiveCache).mock.invocationCallOrder[0]).toBeLessThan(vi.mocked(getDocumentAsync).mock.invocationCallOrder[0]);
 expect(getDocumentAsync).toHaveBeenCalledWith(expect.objectContaining({multiple:false,copyToCacheDirectory:true}));
 expect(source?.name).toBe('archive.zip');expect(source?.sha256).toMatch(/^[a-f0-9]{64}$/);expect(release).not.toHaveBeenCalled();
 releaseArchiveSource(source!);expect(release).toHaveBeenCalledOnce();expect(state.files.has(stagedURI)).toBe(false);
});
it('cleans the picker copy if cancellation arrives while the OS picker is open',async()=>{
 const controller=new AbortController();vi.mocked(getDocumentAsync).mockImplementation(async()=>{controller.abort();return {canceled:false,assets:[{uri:pickerURI,name:'archive.zip',size:22,lastModified:1}]};});
 await expect(pickInterchangeArchive(controller.signal,vi.fn())).rejects.toBeDefined();expect(stageArchiveCache).not.toHaveBeenCalled();expect(state.files.has(pickerURI)).toBe(false);
});
it('releases a staged copy when native hashing fails',async()=>{
 state.short=true;await expect(pickInterchangeArchive(new AbortController().signal,vi.fn())).rejects.toThrow('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');
 expect(release).toHaveBeenCalledOnce();expect(state.files.has(stagedURI)).toBe(false);
});
it('preserves both preparation and cleanup errors',async()=>{
 state.short=true;state.failRelease=true;
 let failure:unknown;try{await pickInterchangeArchive(new AbortController().signal,vi.fn());}catch(error){failure=error;}
 expect(failure).toBeInstanceOf(AggregateError);const errors=(failure as AggregateError).errors;
 expect(errors).toHaveLength(2);expect(errors[0].message).toBe('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');expect(errors[1].message).toBe('delete failed');
});
it('refuses external originals without moving or deleting them',async()=>{
 const uri='file:///documents/original.zip';state.files.add(uri);
 vi.mocked(getDocumentAsync).mockResolvedValue({canceled:false,assets:[{uri,name:'archive.zip',size:22,lastModified:1}]});
 await expect(pickInterchangeArchive(new AbortController().signal,vi.fn())).rejects.toThrow('INTERCHANGE_ARCHIVE_CACHE_SCOPE_INVALID');
 expect(stageArchiveCache).not.toHaveBeenCalled();expect(state.files.has(uri)).toBe(true);
});
it('does not stage a cancelled picker result',async()=>{
 vi.mocked(getDocumentAsync).mockResolvedValue({canceled:true,assets:null});
 await expect(pickInterchangeArchive(new AbortController().signal,vi.fn())).resolves.toBeNull();expect(stageArchiveCache).not.toHaveBeenCalled();
});
