import {useLocale} from '../../src/locale-context';
import {Button, Card, List, Text} from 'react-native-paper';
import {useMemo} from 'react';
import {useLocalSearchParams, useRouter} from '../../src/localized-router';
import {Linking, SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';

// Local DTO — mirrors legal onboarding endpoint (DTO 09 §7).
// Screen is shown when reacceptance_required is true on active document versions.
type LegalDocForReview = {document_key: string; version: string; url: string; reacceptance_required: boolean};
type LegalOnboardingData = {
  docs_requiring_acceptance: LegalDocForReview[];
  acceptance_token: string;
};

export default function LegalOnboarding() {
  const {t} = useLocale();
  const {tenant} = useLocalSearchParams<{tenant: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);

  const resource = useReadResource(
    JSON.stringify([auth.origin, tenant, 'legal-onboarding']),
    api,
    async signal => {
      if (!tenant) throw new Error('Workspace required');
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      return (api as any).legalOnboarding(signal) as Promise<LegalOnboardingData>;
    },
  );

  const data = resource.data;
  const router = useRouter();

  const docLabel = (key: string) => {
    const map: Record<string, string> = {
      terms: t('legal_onboarding_doc_terms'),
      privacy: t('legal_onboarding_doc_privacy'),
    };
    return map[key] ?? key;
  };

  const handleAccept = async () => {
    if (!data) return;
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    await (api as any).legalAcceptAll(data.acceptance_token);
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (router.back as any)();
  };

  const handleDecline = () => {
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    (router.back as any)();
  };

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('legal_onboarding_title')}</Text>
        <Text style={s.meta}>{t('legal_onboarding_intro')}</Text>
        <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel} />
        {data && (
          <Card style={{marginBottom: 8}}>
            <Card.Content>
              {data.docs_requiring_acceptance.map(doc => (
                <List.Item
                  key={doc.document_key}
                  title={docLabel(doc.document_key)}
                  description={doc.version}
                  onPress={() => Linking.openURL(doc.url)}
                  right={() => <Button compact onPress={() => Linking.openURL(doc.url)}>{t('legal_onboarding_view')}</Button>}
                />
              ))}
            </Card.Content>
          </Card>
        )}
        {data && (
          <View style={{flexDirection: 'row', gap: 8}}>
            <Button mode="outlined" onPress={handleDecline}>{t('legal_onboarding_decline')}</Button>
            <Button mode="contained" onPress={handleAccept}>{t('legal_onboarding_accept')}</Button>
          </View>
        )}
      </View>
    </SafeAreaView>
  );
}
