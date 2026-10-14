import {useMemo} from 'react';
import {useLocalSearchParams} from 'expo-router';
import {SafeAreaView,ScrollView} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {PerspectiveReplacementReview} from '../../src/perspective-replacement-review';
import {catalogStyle as s} from '../../src/catalog-style';
export default function PerspectiveReplacementRoute(){
 const {tenant,project,review,original,decisionHash}=useLocalSearchParams<{tenant:string;project:string;review:string;original:string;decisionHash:string}>();
 const auth=useAuth(),api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={s.header}><PerspectiveReplacementReview api={api} origin={auth.origin} tenant={tenant||''} project={project||''} review={review||''} originalTask={original||''} decisionHash={decisionHash||''}/></ScrollView></SafeAreaView>;
}
