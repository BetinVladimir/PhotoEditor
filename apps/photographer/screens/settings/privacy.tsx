import {useLocale} from '../../src/locale-context';
import {Button, Card, Divider, List, Text} from 'react-native-paper';
import {useMemo} from 'react';
import {useLocalSearchParams} from '../../src/localized-router';
import {Linking, SafeAreaView, View} from 'react-native';
import {useAuth} from '../../src/auth-context';
import {OwnerAPI} from '../../src/owner-api';
import {useReadResource} from '../../src/use-read-resource';
import {ReadResourceState} from '../../src/read-resource-state';
import {catalogStyle as s} from '../../src/catalog-style';

// Local DTO — mirrors privacy settings endpoint (DTO 09 §7).
type LegalDocRef = {document_key: string; locale: string; version: string; url: string};
type PrivacySettings = {
  legal_docs: LegalDocRef[];
  can_request_erasure: boolean;
  can_download_data: boolean;
};

const DOC_KEY_LABELS = ['terms', 'privacy', 'cookies', 'dpa'] as const;

export default function Privacy() {
  const {t} = useLocale();
  const {tenant} = useLocalSearchParams<{tenant: string}>();
  const auth = useAuth();
  const api = useMemo(() => new OwnerAPI(auth.client(), tenant || ''), [auth, tenant]);

  const resource = useReadResource(
    JSON.stringify([auth.origin, tenant, 'privacy-settings']),
    api,
    async signal => {
      if (!tenant) throw new Error('Workspace required');
      // eslint-disable-next-line @typescript-eslint/no-explicit-any
      return (api as any).privacySettings(signal) as Promise<PrivacySettings>;
    },
  );

  const data = resource.data;

  const docLabel = (key: string) => {
    const map: Record<string, string> = {
      terms: t('privacy_doc_terms'),
      privacy: t('privacy_doc_privacy'),
      cookies: t('privacy_doc_cookies'),
      dpa: t('privacy_doc_dpa'),
    };
    return map[key] ?? key;
  };

  const handleOpenDoc = (url: string) => {
    Linking.openURL(url);
  };

  const handleRequestErasure = () => {
    // Navigate to erasure request flow
  };

  const handleDownloadData = () => {
    // Trigger data download
  };

  return (
    <SafeAreaView style={s.root}>
      <View style={s.header}>
        <Text variant="headlineMedium" style={s.title}>{t('privacy_title')}</Text>
        <ReadResourceState phase={resource.phase} retry={resource.retry} cancel={resource.cancel} />
        {!data && resource.phase === 'ready' && (
          <Text style={s.empty}>{t('privacy_empty')}</Text>
        )}
        {data && (
          <>
            <Card style={{marginBottom: 8}}>
              <Card.Title title={t('privacy_data_section')} />
              <Card.Content>
                {data.can_download_data && (
                  <Button mode="outlined" onPress={handleDownloadData}>{t('privacy_download_data')}</Button>
                )}
              </Card.Content>
            </Card>
            <Card style={{marginBottom: 8}}>
              <Card.Title title={t('privacy_erasure_section')} />
              <Card.Content>
                {data.can_request_erasure && (
                  <Button mode="outlined" onPress={handleRequestErasure}>{t('privacy_request_erasure')}</Button>
                )}
              </Card.Content>
            </Card>
            <Card style={{marginBottom: 8}}>
              <Card.Title title={t('privacy_legal_docs')} />
              <Card.Content>
                {DOC_KEY_LABELS.map(key => {
                  const doc = data.legal_docs.find(d => d.document_key === key);
                  if (!doc) return null;
                  return (
                    <List.Item
                      key={key}
                      title={docLabel(key)}
                      onPress={() => handleOpenDoc(doc.url)}
                      right={() => <Text style={s.meta}>{doc.version}</Text>}
                    />
                  );
                })}
              </Card.Content>
            </Card>
            <Divider />
          </>
        )}
      </View>
    </SafeAreaView>
  );
}
