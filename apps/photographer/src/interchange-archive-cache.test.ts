import {afterEach,expect,it,vi} from 'vitest';
const state=vi.hoisted(()=>({files:new Set<string>(),directories:new Set<string>(),failDelete:false}));
vi.mock('expo-file-system',()=>{
 class File{
  uri:string;
  constructor(...parts:(string|{uri:string})[]){this.uri=parts.map(p=>typeof p==='string'?p:p.uri).join('/');}
  get exists(){return state.files.has(this.uri);}
  delete(){if(state.failDelete)throw new Error('disk');state.files.delete(this.uri);}
  move(target:File){state.files.delete(this.uri);state.files.add(target.uri);this.uri=target.uri;}
 }
 class Directory{
  uri:string;
  constructor(...parts:(string|{uri:string})[]){this.uri=parts.map(p=>typeof p==='string'?p:p.uri).join('/');}
  get exists(){return state.directories.has(this.uri);}
  create(){state.directories.add(this.uri);}
  list(){return [...state.files].filter(uri=>uri.startsWith(this.uri+'/')).map(uri=>new File(uri));}
 }
 return {File,Directory,Paths:{cache:{uri:'file:///cache'}}};
});
import {File} from 'expo-file-system';
import {stageArchiveCache,sweepArchiveCache} from './interchange-archive-cache';
const directory='file:///cache/photoeditor-interchange-v1';
const orphan=directory+'/00000000-0000-4000-8000-000000000001.zip';
afterEach(()=>{state.files.clear();state.directories.clear();state.failDelete=false;});
it('sweeps owned crash leftovers but preserves external, unknown and nested files',()=>{
 state.directories.add(directory);
 const preserve=['file:///cache/original.zip',directory+'/unknown.zip',directory+'/nested/00000000-0000-4000-8000-000000000002.zip'];
 for(const uri of [orphan,...preserve])state.files.add(uri);
 sweepArchiveCache();expect([...state.files].sort()).toEqual(preserve.sort());
});
it('protects a live staged source until explicitly released',()=>{
 const uri='file:///cache/picker.zip';state.files.add(uri);
 const staged=stageArchiveCache(new File(uri));sweepArchiveCache();expect(staged.file.exists).toBe(true);expect(state.files.has(uri)).toBe(false);
 staged.release();expect(staged.file.exists).toBe(false);staged.release();
});
it('retries a failed release through the next sweep',()=>{
 const uri='file:///cache/picker.zip';state.files.add(uri);const staged=stageArchiveCache(new File(uri));
 state.failDelete=true;expect(()=>staged.release()).toThrow('disk');expect(staged.file.exists).toBe(true);
 state.failDelete=false;sweepArchiveCache();expect(staged.file.exists).toBe(false);
});
it('surfaces sweep failures instead of silently accepting unbounded cache growth',()=>{
 state.directories.add(directory);state.files.add(orphan);state.failDelete=true;
 expect(()=>sweepArchiveCache()).toThrow('disk');expect(state.files.has(orphan)).toBe(true);
});
