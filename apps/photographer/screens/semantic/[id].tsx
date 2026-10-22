import {useEditorCapabilities} from '../../src/use-editor-capabilities';
import {liveSemanticAvailability} from '../../src/semantic-kind-availability';
import AsyncStorage from '@react-native-async-storage/async-storage';
import {OwnerAPI} from '../../src/owner-api';
import {SemanticApplyAttemptStore} from '../../src/semantic-apply-attempt-store';
import {useCallback,useMemo} from 'react';
import {useLocalSearchParams,useRouter} from '../../src/localized-router';
import {useAuth} from '../../src/auth-context';
import {SemanticTargetScreen} from '../../src/semantic-target-screen';
export default function SemanticTargets(){
 const{tenant,id}=useLocalSearchParams<{tenant:string;id:string}>();const router=useRouter();const auth=useAuth();const client=useMemo(()=>auth.client(),[auth]);
 const owner=useMemo(()=>new OwnerAPI(client,tenant||''),[client,tenant]);
 const capabilityError=useCallback(()=>{},[]);
 const {current:capabilityState}=useEditorCapabilities(owner,tenant||'',capabilityError);
 const availability=useMemo(()=>liveSemanticAvailability(capabilityState),[capabilityState]);
 const journalFactory=useCallback(async(signal:AbortSignal)=>{const user=await new OwnerAPI(client,tenant||'').me(signal);signal.throwIfAborted();return new SemanticApplyAttemptStore(AsyncStorage,client.origin,user.id,tenant||'',id||'');},[client,tenant,id]);
 const onOpenVersion=useCallback((version:string,asset:string,project:string)=>router.push({pathname:'/editor/[version]',params:{version,asset,project,tenant:tenant||''}}),[router,tenant]);
 return <SemanticTargetScreen actionAvailable={availability.actionAvailable} key={`${tenant}:${id}`} client={client} onOpenVersion={onOpenVersion} journalFactory={journalFactory} tenant={tenant||''} intent={id||''}/>;
}
