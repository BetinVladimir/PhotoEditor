import {freezeCollectionVersion,readCollectionVersionSelection,type CollectionVersionSelection} from './collection-version';
export interface CollectionVersionTransport{
 reconcileCollectionVersion(selection:CollectionVersionSelection,signal?:AbortSignal):Promise<'applied'|'unchanged'|'conflict'>;
 replaceCollectionVersion(selection:CollectionVersionSelection,signal?:AbortSignal):Promise<unknown>;
}
// Call only after persisting the immutable selection intent. Reads before every
// mutation, including first execution, to recover lost responses without a new CAS.
export async function resumeCollectionVersion(api:CollectionVersionTransport,selection:CollectionVersionSelection,signal?:AbortSignal){
 const owned=freezeCollectionVersion(selection),active=()=>signal?.throwIfAborted();active();
 let state=await api.reconcileCollectionVersion(structuredClone(owned),signal);active();
 if(state==='conflict')throw new Error('COLLECTION_VERSION_CONFLICT');
 if(state==='applied')return;
 try{readCollectionVersionSelection(await api.replaceCollectionVersion(structuredClone(owned),signal),owned);active();}
 catch(error){active();state=await api.reconcileCollectionVersion(structuredClone(owned),signal);active();if(state!=='applied')throw error;}
}
