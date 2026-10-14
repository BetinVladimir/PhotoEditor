import {useMemo} from 'react';
import {useLocalSearchParams} from 'expo-router';
import {useAuth} from '../../src/auth-context';
import {SemanticTargetScreen} from '../../src/semantic-target-screen';
export default function SemanticTargets(){
 const{tenant,id}=useLocalSearchParams<{tenant:string;id:string}>();const auth=useAuth();const client=useMemo(()=>auth.client(),[auth]);
 return <SemanticTargetScreen key={`${tenant}:${id}`} client={client} tenant={tenant||''} intent={id||''}/>;
}
