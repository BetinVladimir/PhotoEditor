import {router,useLocalSearchParams} from 'expo-router';
import {InterchangeImportFormatScreen} from '../../src/interchange-import-format-screen';
export default function InterchangeImportFormatRoute(){
 const {tenant,project}=useLocalSearchParams<{tenant:string;project:string}>();
 return <InterchangeImportFormatScreen key={JSON.stringify([tenant,project])} tenant={tenant} project={project} onSelect={format=>router.push({pathname:'/interchange/upload',params:{tenant,project,format}})}/>;
}
