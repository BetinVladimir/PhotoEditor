import {Text} from 'react-native-paper';
import {useMemo} from 'react';

import {useLocalSearchParams} from '../../../src/localized-router';
import {useAuth} from '../../../src/auth-context';
import {useLocale} from '../../../src/locale-context';
import {assertInterchangeScope} from '../../../src/interchange-job';
import {InterchangePersonMaskScreen} from '../../../src/interchange-person-mask-screen';

export default function InterchangeMasksRoute(){
  const {tenant,project,id}=useLocalSearchParams<{tenant:string;project:string;id:string}>();
  const auth=useAuth(),client=useMemo(()=>auth.client(),[auth]),{t}=useLocale();
  try{assertInterchangeScope(tenant||'',project||'',id||'');}
  catch{return <Text accessibilityRole="alert">{t('interchange_error_job_unavailable')}</Text>;}
  return <InterchangePersonMaskScreen client={client} tenant={tenant!} project={project!} workflow={id!}/>;
}
