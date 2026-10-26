import {Stack,useLocalSearchParams,usePathname} from 'expo-router';
import {isDeferredInMvp} from '../../src/mvp-profile';
import {useLocale} from '../../src/locale-context';
import {Text} from 'react-native-paper';
import {WorkspaceAccessBoundary} from '../../src/workspace-access-context';
import {isURLLocale} from '../../src/locale-url';
export default function LocalizedLayout(){const {t}=useLocale();const {locale}=useLocalSearchParams();const pathname=usePathname();if(!isURLLocale(locale))return <Text accessibilityRole="alert">{t('read_error')}</Text>;if(isDeferredInMvp(pathname))return <Text accessibilityRole="alert">{t('feature_not_in_mvp')}</Text>;return <Stack screenOptions={{headerShown:false}} screenLayout={({children,route})=><WorkspaceAccessBoundary routeName={route.name} params={{...route.params}}>{children}</WorkspaceAccessBoundary>}/>}
