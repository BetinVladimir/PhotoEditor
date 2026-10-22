import {Text} from 'react-native-paper';
import {useMemo} from 'react';
import {useLocalSearchParams} from '../../src/localized-router';
import {SafeAreaView,ScrollView} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {MultiReplacementReview} from '../../src/multi-replacement-review';
import {catalogStyle as s} from '../../src/catalog-style';
import {useLocale} from '../../src/locale-context';
export default function MultiReplacementRoute(){
 const {tenant,project,review,original,decisionHash,operation}=useLocalSearchParams<{tenant:string;project:string;review:string;original:string;decisionHash:string;operation:string}>();
 const auth=useAuth(),{t}=useLocale(),api=useMemo(()=>new OwnerAPI(auth.client(),tenant||''),[auth,tenant]);
 if(operation!=='optical.panorama'&&operation!=='optical.focus_stack')return <SafeAreaView style={s.root}><Text accessibilityRole="alert">{t('multi_review_error')}</Text></SafeAreaView>;
 return <SafeAreaView style={s.root}><ScrollView contentContainerStyle={s.header}><MultiReplacementReview api={api} origin={auth.origin} tenant={tenant||''} project={project||''} review={review||''} originalTask={original||''} decisionHash={decisionHash||''} operation={operation}/></ScrollView></SafeAreaView>;
}
