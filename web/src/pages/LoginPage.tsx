import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { Alert, Button, Card, Form, Input, Tabs } from 'antd';
import { KeyOutlined, LockOutlined } from '@ant-design/icons';
import { PreferencesDropdown } from '@/components/ui/PreferencesDropdown';
import { BrandLink } from '@/components/BrandLink';
import { AppShell } from '@/components/layout';
import { useEmbed } from '@/embed/EmbedContext';
import styles from './LoginPage.module.scss';

type LoginMode = 'admin' | 'api_key';

type LoginErrors = {
  adminError?: string;
  apiKeyError?: string;
};

interface LoginPageProps extends LoginErrors {
  loading?: boolean;
  onPasswordSubmit: (password: string) => Promise<void>;
  onAPIKeySubmit: (apiKey: string) => Promise<void>;
}

export const getLoginErrorForMode = (mode: LoginMode, { adminError = '', apiKeyError = '' }: LoginErrors) => (
  mode === 'api_key' ? apiKeyError : adminError
);

export function LoginPage({ loading = false, adminError = '', apiKeyError = '', onPasswordSubmit, onAPIKeySubmit }: LoginPageProps) {
  const { t } = useTranslation();
  const embedded = useEmbed();
  const [mode, setMode] = useState<LoginMode>('admin');
  const [password, setPassword] = useState('');
  const [apiKey, setApiKey] = useState('');
  const activeError = getLoginErrorForMode(mode, { adminError, apiKeyError });

  const canSubmit = mode === 'api_key' ? Boolean(apiKey.trim()) : Boolean(password.trim());
  const handleSubmit = async () => {
    if (mode === 'api_key') {
      await onAPIKeySubmit(apiKey);
      return;
    }
    await onPasswordSubmit(password);
  };

  return (
    <AppShell
      className={styles.guestShell}
      variant={embedded ? 'embed' : 'guest'}
      sticky="static"
      nav={{ mode: 'none' }}
      slots={{
        brand: <BrandLink className={styles.brandLink} />,
        headerTitle: t('auth.login_title'),
        headerUtility: <PreferencesDropdown />,
        content: (
          <div className={styles.loginColumn}>
            <Card className={styles.loginCard} variant="outlined">
              <Tabs
                className={styles.loginTabs}
                activeKey={mode}
                onChange={(key) => setMode(key as LoginMode)}
                items={[
                  { key: 'admin', label: t('auth.admin_tab'), icon: <LockOutlined />, disabled: loading },
                  { key: 'api_key', label: t('auth.api_key_tab'), icon: <KeyOutlined />, disabled: loading },
                ]}
              />

              {activeError && (
                <Alert className={styles.loginError} type="error" message={activeError} showIcon />
              )}

              <Form className={styles.form} layout="vertical" requiredMark={false} onFinish={() => void handleSubmit()}>
                {mode === 'api_key' ? (
                  <Form.Item label={t('auth.api_key_label')}>
                    <Input.Password
                      autoComplete="off"
                      placeholder={t('auth.api_key_placeholder')}
                      value={apiKey}
                      onChange={(event) => setApiKey(event.target.value)}
                      disabled={loading}
                      status={activeError ? 'error' : undefined}
                      prefix={<KeyOutlined />}
                    />
                  </Form.Item>
                ) : (
                  <Form.Item label={t('auth.password_label')}>
                    <Input.Password
                      autoComplete="current-password"
                      placeholder={t('auth.password_placeholder')}
                      value={password}
                      onChange={(event) => setPassword(event.target.value)}
                      disabled={loading}
                      status={activeError ? 'error' : undefined}
                      prefix={<LockOutlined />}
                    />
                  </Form.Item>
                )}
                <Button
                  type="primary"
                  htmlType="submit"
                  block
                  loading={loading}
                  disabled={!canSubmit}
                  icon={mode === 'api_key' ? <KeyOutlined /> : <LockOutlined />}
                >
                  {mode === 'api_key' ? t('auth.api_key_login_submit') : t('auth.login_submit')}
                </Button>
              </Form>
            </Card>
          </div>
        ),
      }}
    />
  );
}
