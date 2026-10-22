import {Stack,useLocalSearchParams} from 'expo-router';
import {useLocale} from '../../src/locale-context';
import {Text} from 'react-native-paper';
import {WorkspaceAccessBoundary} from '../../src/workspace-access-context';
import {isURLLocale} from '../../src/locale-url';
export default function LocalizedLayout(){const {t}=useLocale();const {locale}=useLocalSearchParams();if(!isURLLocale(locale))return <Text accessibilityRole="alert">{t('read_error')}</Text>;return <Stack screenOptions={{headerShown:false}} screenLayout={({children,route})=><WorkspaceAccessBoundary routeName={route.name} params={{...route.params}}>{children}</WorkspaceAccessBoundary>}/>}
