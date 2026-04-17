import { useState, type FormEvent } from 'react';
import { useTranslation } from 'react-i18next';
import { Button } from '@/components/ui/Button';
import { Card } from '@/components/ui/Card';
import { Input } from '@/components/ui/Input';
import styles from './LoginPage.module.scss';

interface LoginPageProps {
  loading?: boolean;
  error?: string;
  onSubmit: (password: string) => Promise<void>;
}

export function LoginPage({ loading = false, error = '', onSubmit }: LoginPageProps) {
  const { t } = useTranslation();
  const [password, setPassword] = useState('');

  const handleSubmit = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    await onSubmit(password);
  };

  return (
    <div className={styles.pageShell}>
      <div className={styles.frame}>
        <div className={styles.brandBlock}>
          <span className={styles.eyebrow}>CPA Usage Keeper</span>
          <h1 className={styles.title}>{t('auth.login_title')}</h1>
          <p className={styles.subtitle}>{t('auth.login_subtitle')}</p>
        </div>

        <Card className={styles.loginCard}>
          <form className={styles.form} onSubmit={(event) => void handleSubmit(event)}>
            <Input
              type="password"
              autoComplete="current-password"
              label={t('auth.password_label')}
              placeholder={t('auth.password_placeholder')}
              value={password}
              onChange={(event) => setPassword(event.target.value)}
              error={error || undefined}
              disabled={loading}
            />
            <Button type="submit" fullWidth loading={loading} disabled={!password.trim()}>
              {t('auth.login_submit')}
            </Button>
          </form>
        </Card>
      </div>
    </div>
  );
}
