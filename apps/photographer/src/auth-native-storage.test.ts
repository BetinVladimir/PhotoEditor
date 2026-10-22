import {afterEach,expect,it,vi} from 'vitest';
const storage=vi.hoisted(()=>({values:new Map<string,string>(),set:vi.fn(),get:vi.fn(),remove:vi.fn()}));
vi.mock('expo-secure-store',()=>({setItemAsync:storage.set,getItemAsync:storage.get,deleteItemAsync:storage.remove}));
import {OwnerAuth} from './auth';
function reset(){storage.set.mockImplementation(async(key:string,value:string)=>{storage.values.set(key,value);});storage.get.mockImplementation(async(key:string)=>storage.values.get(key)??null);storage.remove.mockImplementation(async(key:string)=>{storage.values.delete(key);});}
afterEach(()=>{storage.values.clear();vi.resetAllMocks();});
it('serializes logout after pending native writes and removes the late envelope',async()=>{
 reset();const pending:Array<()=>void>=[];storage.set.mockImplementation((key:string,value:string)=>new Promise<void>(resolve=>{pending.push(()=>{storage.values.set(key,value);resolve();});}));
 const auth=new OwnerAuth('https://native.test','native');auth.setTokens({access_token:'access',refresh_token:'refresh',csrf_token:'csrf'});
 const write=auth.persist(),rejection=expect(write).rejects.toMatchObject({name:'AbortError'});await Promise.resolve();await Promise.resolve();expect(pending).toHaveLength(2);
 const logout=auth.clear();pending.forEach(done=>done());await rejection;await logout;expect(storage.values.size).toBe(0);expect(auth.principal()).toBeNull();
});
it('cleans an old write before persisting the newer login envelope',async()=>{
 reset();const pending:Array<()=>void>=[];let calls=0;storage.set.mockImplementation((key:string,value:string)=>{calls++;if(calls<=2)return new Promise<void>(resolve=>{pending.push(()=>{storage.values.set(key,value);resolve();});});storage.values.set(key,value);return Promise.resolve();});
 const auth=new OwnerAuth('https://native.test','native');auth.setTokens({access_token:'old',refresh_token:'old-refresh',csrf_token:'old-csrf'});
 const old=auth.persist(),rejection=expect(old).rejects.toMatchObject({name:'AbortError'});await Promise.resolve();await Promise.resolve();
 const fresh=auth.acceptLoginTokens({access_token:'new',refresh_token:'new-refresh',csrf_token:'new-csrf'});pending.forEach(done=>done());await rejection;await fresh;
 expect(storage.values.get('photoeditor.refresh.v1')).toBe('new-refresh');expect(storage.values.get('photoeditor.csrf.v1')).toBe('new-csrf');
});
it('cleans both keys when only one native credential write succeeds',async()=>{
 reset();storage.set.mockImplementation(async(key:string,value:string)=>{if(key.includes('csrf'))throw new Error('storage full');storage.values.set(key,value);});
 const auth=new OwnerAuth('https://native.test','native');await expect(auth.acceptLoginTokens({access_token:'access',refresh_token:'refresh',csrf_token:'csrf'})).rejects.toThrow('storage full');expect(storage.values.size).toBe(0);
});
it('rejects an incomplete native login before replacing the current principal',async()=>{
 reset();const auth=new OwnerAuth('https://native.test','native');await expect(auth.acceptLoginTokens({access_token:'access',csrf_token:'csrf'})).rejects.toThrow('native token envelope incomplete');expect(auth.principal()).toBeNull();expect(storage.set).not.toHaveBeenCalled();
});
it('waits for the other writer to settle before cleaning a partially failed envelope',async()=>{
 reset();let finish!:(()=>void);storage.set.mockImplementation((key:string,value:string)=>key.includes('csrf')?Promise.reject(new Error('storage full')):new Promise<void>(resolve=>{finish=()=>{storage.values.set(key,value);resolve();};}));
 const auth=new OwnerAuth('https://native.test','native');const accepted=auth.acceptLoginTokens({access_token:'access',refresh_token:'refresh',csrf_token:'csrf'}),rejection=expect(accepted).rejects.toThrow('storage full');
 await Promise.resolve();await Promise.resolve();await Promise.resolve();expect(storage.remove).not.toHaveBeenCalled();finish();await rejection;expect(storage.values.size).toBe(0);
});
it('does not release a failed cleanup until its other delete settles before the next login',async()=>{
 reset();let deleteStarted!:()=>void,finishDelete!:()=>void;const deleting=new Promise<void>(resolve=>{deleteStarted=resolve;});let writes=0;
 storage.set.mockImplementation(async(key:string,value:string)=>{writes++;if(writes<=2&&key.includes('csrf'))throw new Error('write failed');storage.values.set(key,value);});
 storage.remove.mockImplementation((key:string)=>{if(key.includes('refresh'))return Promise.reject(new Error('delete failed'));deleteStarted();return new Promise<void>(resolve=>{finishDelete=()=>{storage.values.delete(key);resolve();};});});
 const auth=new OwnerAuth('https://native.test','native');const prior=auth.acceptLoginTokens({access_token:'old',refresh_token:'old-refresh',csrf_token:'old-csrf'}),rejection=expect(prior).rejects.toThrow('delete failed');await deleting;
 const fresh=auth.acceptLoginTokens({access_token:'new',refresh_token:'new-refresh',csrf_token:'new-csrf'});await Promise.resolve();expect(writes).toBe(2);
 finishDelete();await rejection;await fresh;expect(storage.values.get('photoeditor.refresh.v1')).toBe('new-refresh');expect(storage.values.get('photoeditor.csrf.v1')).toBe('new-csrf');
});

it('cancels a hanging acceptance promptly without publishing a principal, then cleans late writes',async()=>{
 reset();const pending:Array<()=>void>=[];storage.set.mockImplementation((key:string,value:string)=>new Promise<void>(resolve=>{pending.push(()=>{storage.values.set(key,value);resolve();});}));
 const auth=new OwnerAuth('https://native.test','native'),controller=new AbortController();
 const acceptance=auth.acceptLoginTokens({access_token:'pending',refresh_token:'refresh',csrf_token:'csrf'},controller.signal),rejected=expect(acceptance).rejects.toMatchObject({name:'AbortError'});
 await Promise.resolve();await Promise.resolve();expect(pending).toHaveLength(2);expect(auth.principal()).toBeNull();
 controller.abort();await rejected;expect(auth.principal()).toBeNull();pending.forEach(done=>done());await auth.clear();expect(storage.values.size).toBe(0);
});
it('does not revoke a newer external session when an older acceptance is cancelled',async()=>{
 reset();const pending:Array<()=>void>=[];storage.set.mockImplementation((key:string,value:string)=>new Promise<void>(resolve=>{pending.push(()=>{storage.values.set(key,value);resolve();});}));
 const auth=new OwnerAuth('https://native.test','native'),controller=new AbortController();
 const acceptance=auth.acceptLoginTokens({access_token:'pending',refresh_token:'refresh',csrf_token:'csrf'},controller.signal),rejected=expect(acceptance).rejects.toMatchObject({name:'AbortError'});
 await Promise.resolve();await Promise.resolve();auth.setTokens({access_token:'external',refresh_token:'external-refresh',csrf_token:'external-csrf'});controller.abort();await rejected;
 expect(auth.principal()).toEqual({kind:'owner',accessToken:'external'});pending.forEach(done=>done());await auth.clear();
});
