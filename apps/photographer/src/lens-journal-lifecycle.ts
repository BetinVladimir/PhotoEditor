import {semanticJournalLock} from './semantic-attempt-store';
type Storage={getItem(key:string):Promise<string|null>;setItem(key:string,value:string):Promise<void>};
const epochs=new Map<string,number>();
const generationKey=(origin:string)=>`photo.lens.generation.v1:${JSON.stringify(origin)}`;
export function lensJournalFence(storage:Storage,origin:string){
 const epoch=epochs.get(origin)??0;let generation:string|null|undefined;
 return async()=>{
  if(epoch!==(epochs.get(origin)??0))throw new Error('LENS_JOURNAL_EXPIRED');
  const value=await storage.getItem(generationKey(origin));
  if(epoch!==(epochs.get(origin)??0))throw new Error('LENS_JOURNAL_EXPIRED');
  if(value!==null&&!/^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/.test(value))throw new Error('LENS_JOURNAL_INVALID');
  if(generation===undefined)generation=value;else if(generation!==value)throw new Error('LENS_JOURNAL_EXPIRED');
 };
}
let pending:Promise<unknown>=Promise.resolve();
export function lensJournalLock<T>(origin:string,action:()=>Promise<T>){const next=pending.catch(()=>{}).then(()=>semanticJournalLock(origin,action));pending=next;return next;}
export function clearLensAttempts(storage:Storage&{getAllKeys():Promise<readonly string[]>;removeItem(key:string):Promise<void>},origin:string){
 epochs.set(origin,(epochs.get(origin)??0)+1);
 return lensJournalLock(origin,async()=>{
  await storage.setItem(generationKey(origin),crypto.randomUUID());
  const scopes=['photo.lens.preparation.v1:','photo.lens.apply.v1:'].map(prefix=>prefix+'['+JSON.stringify(origin)+',');
  for(const key of await storage.getAllKeys())if(scopes.some(prefix=>key.startsWith(prefix)))await storage.removeItem(key);
 });
}
