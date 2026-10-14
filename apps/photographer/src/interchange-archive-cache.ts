import {Directory,File,Paths} from 'expo-file-system';
const active=new Set<string>();
const ownedName=/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.zip$/;
function root(){return new Directory(Paths.cache,'photoeditor-interchange-v1');}
/** Synchronous sweep cannot interleave with registration of another selection. */
export function sweepArchiveCache():void {
 const directory=root();if(!directory.exists)return;
 const prefix=directory.uri.endsWith('/')?directory.uri:directory.uri+'/';
 for(const entry of directory.list()){
  if(!(entry instanceof File)||active.has(entry.uri))continue;
  const name=entry.uri.slice(prefix.length);
  if(!entry.uri.startsWith(prefix)||!ownedName.test(name))continue;
  if(entry.exists)entry.delete();
 }
}
/** Move only the already-validated picker cache copy into our private namespace. */
export function stageArchiveCache(copy:File):{file:File;release:()=>void}{
 const directory=root();directory.create({intermediates:true,idempotent:true});
 const file=new File(directory,crypto.randomUUID()+'.zip');active.add(file.uri);
 try{copy.move(file);}catch(error){active.delete(file.uri);throw error;}
 const uri=file.uri;
 return {file,release:()=>{active.delete(uri);const cached=new File(uri);if(cached.exists)cached.delete();}};
}
