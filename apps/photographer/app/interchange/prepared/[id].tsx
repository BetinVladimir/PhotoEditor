import {useMemo} from 'react';
import {Text} from 'react-native';
import {useLocalSearchParams} from 'expo-router';
import {useAuth} from '../../../src/auth-context';
import {useLocale} from '../../../src/locale-context';
import {assertInterchangeScope} from '../../../src/interchange-job';
import {InterchangePreparedReviewScreen} from '../../../src/interchange-prepared-review-screen';

export default function InterchangePreparedReviewRoute(){
 const {tenant,project,id}=useLocalSearchParams<{tenant:string;project:string;id:string}>();
 const auth=useAuth(),client=useMemo(()=>auth.client(),[auth]),{t}=useLocale();
 try{assertInterchangeScope(tenant||'',project||'',id||'');}
 catch{return <Text accessibilityRole="alert">{t('interchange_error_job_unavailable')}</Text>;}
 return <InterchangePreparedReviewScreen client={client} tenant={tenant!} project={project!} workflow={id!}/>;
}
