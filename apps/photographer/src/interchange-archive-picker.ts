import {stageArchiveCache,sweepArchiveCache} from './interchange-archive-cache';
import {ownArchiveSource} from './interchange-archive-source-lifecycle';
import {Platform} from 'react-native';
import * as DocumentPicker from 'expo-document-picker';
import {File as NativeFile,Paths} from 'expo-file-system';
import {interchangeArchiveSource} from './interchange-archive-source';
import {pickWebArchive,type ArchivePicker} from './interchange-archive-screen';
export async function nativeArchiveSource(uri:string,name:string,size:number,signal:AbortSignal,onHash?:(done:number,total:number)=>void){
 signal.throwIfAborted();const initial=new NativeFile(uri),modified=initial.lastModified;
 if(!initial.exists||initial.size!==size)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');
 const stable=()=>{const file=new NativeFile(uri);if(!file.exists||file.size!==size||file.lastModified!==modified)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');return file;};
 return interchangeArchiveSource({name,size,lastModified:modified??0,slice:(start,end)=>({arrayBuffer:async()=>{
  // Native handle reads are synchronous: yield between chunks for cancel/UI.
  await new Promise<void>(resolve=>setTimeout(resolve,0));signal.throwIfAborted();
  const handle=stable().open();let bytes:Uint8Array;
  try{handle.offset=start;bytes=handle.readBytes(end-start);}finally{handle.close();}
  signal.throwIfAborted();stable();if(bytes.byteLength!==end-start)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_CHANGED');
  const owned=new Uint8Array(bytes.byteLength);owned.set(bytes);return owned.buffer;
 }})},signal,onHash);
}
export function prepareArchiveCache(signal:AbortSignal):void{
 signal.throwIfAborted();if(Platform.OS!=='web')sweepArchiveCache();signal.throwIfAborted();
}
export const pickInterchangeArchive:ArchivePicker=async(signal,onHash)=>{
 signal.throwIfAborted();if(Platform.OS==='web')return pickWebArchive(signal,onHash);
 prepareArchiveCache(signal);
 const picked=await DocumentPicker.getDocumentAsync({type:['application/zip','application/x-zip-compressed'],multiple:false,copyToCacheDirectory:true});
 if(picked.canceled){signal.throwIfAborted();return null;}
 // Only delete the picker-created cache copy, inside the configured cache root.
 const cache=new URL(Paths.cache.uri.endsWith('/')?Paths.cache.uri:Paths.cache.uri+'/');
 const copies=picked.assets.map(file=>{const url=new URL(file.uri);if(url.protocol!==cache.protocol||url.host!==cache.host||!url.pathname.startsWith(cache.pathname)||url.pathname===cache.pathname||url.search||url.hash)throw new Error('INTERCHANGE_ARCHIVE_CACHE_SCOPE_INVALID');return new NativeFile(file.uri);});
 let staged:ReturnType<typeof stageArchiveCache>|null=null;
 const release=()=>{staged?.release();for(const copy of copies){if(copy.exists)copy.delete();}};
 try{
  signal.throwIfAborted();if(picked.assets.length!==1)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_INVALID');
  const file=picked.assets[0];if(!Number.isSafeInteger(file.size)||!file.size)throw new Error('INTERCHANGE_ARCHIVE_SOURCE_INVALID');
  staged=stageArchiveCache(copies[0]);
  const source=await nativeArchiveSource(staged.file.uri,file.name,file.size,signal,onHash);signal.throwIfAborted();
  return ownArchiveSource(source,release);
 }catch(error){
  try{release();}catch(cleanupError){throw new AggregateError([error,cleanupError],'INTERCHANGE_ARCHIVE_PREPARATION_AND_CLEANUP_FAILED');}
  throw error;
 }
};
