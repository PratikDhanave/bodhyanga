import { useState, type FormEvent } from 'react';
import { useNavigate } from 'react-router-dom';
import { setToken } from '../hooks/useAuth';
import { useI18n } from '../i18n';

export default function Login() {
  const navigate = useNavigate();
  const { t } = useI18n();
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [showPassword, setShowPassword] = useState(false);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setLoading(true);
    setError(null);
    try {
      const res = await fetch('/api/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      });
      if (!res.ok) {
        if (res.status === 429) {
          setError(t('login.error.rate_limit'));
        } else if (res.status >= 500) {
          setError(t('login.error.server'));
        } else {
          setError(t('login.error'));
        }
        return;
      }
      const data = await res.json() as { token?: string };
      if (!data.token) {
        setError(t('login.error'));
        return;
      }
      setToken(data.token);
      navigate('/');
    } catch {
      setError(t('login.error.network'));
    } finally {
      setLoading(false);
    }
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-bodh-50 dark:bg-gray-900">
      <div className="w-full max-w-sm">
        {/* Branding */}
        <div className="text-center mb-8">
          <div className="inline-flex items-center justify-center w-14 h-14 rounded-2xl bg-bodh-800 dark:bg-bodh-500 text-white text-2xl font-bold mb-3">
            B
          </div>
          <h1 className="text-2xl font-semibold text-bodh-800 dark:text-gray-100">Bodh</h1>
          <p className="text-sm text-bodh-400 dark:text-gray-500 mt-1">{t('login.subtitle')}</p>
        </div>

        {/* Form card */}
        <div className="card">
          <div className="card-body space-y-4">
            <div>
              <h2 className="text-lg font-medium text-bodh-800 dark:text-gray-100">{t('login.title')}</h2>
            </div>

            <form onSubmit={(e) => { void handleSubmit(e); }} className="space-y-4">
              {/* Email */}
              <div>
                <label htmlFor="email" className="block text-sm font-medium text-bodh-700 dark:text-gray-300 mb-1">
                  {t('login.email')}
                </label>
                <input
                  id="email"
                  type="email"
                  autoComplete="email"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  className="w-full px-3 py-2 border border-bodh-200 dark:border-gray-600 rounded-md text-sm text-bodh-800 dark:text-gray-100 bg-white dark:bg-gray-800 placeholder-bodh-300 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-bodh-400 focus:border-transparent"
                  placeholder="clinician@hospital.org"
                />
              </div>

              {/* Password */}
              <div>
                <label htmlFor="password" className="block text-sm font-medium text-bodh-700 dark:text-gray-300 mb-1">
                  {t('login.password')}
                </label>
                <div className="relative">
                  <input
                    id="password"
                    type={showPassword ? 'text' : 'password'}
                    autoComplete="current-password"
                    required
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="w-full px-3 py-2 pr-10 border border-bodh-200 dark:border-gray-600 rounded-md text-sm text-bodh-800 dark:text-gray-100 bg-white dark:bg-gray-800 placeholder-bodh-300 dark:placeholder-gray-500 focus:outline-none focus:ring-2 focus:ring-bodh-400 focus:border-transparent"
                    placeholder="••••••••"
                  />
                  <button
                    type="button"
                    onClick={() => setShowPassword((v) => !v)}
                    className="absolute inset-y-0 right-0 px-3 flex items-center text-bodh-400 dark:text-gray-500 hover:text-bodh-600 dark:hover:text-gray-300"
                    aria-label={showPassword ? 'Hide password' : 'Show password'}
                  >
                    {showPassword ? (
                      <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M13.875 18.825A10.05 10.05 0 0112 19c-4.478 0-8.268-2.943-9.543-7a9.97 9.97 0 011.563-3.029m5.858.908a3 3 0 114.243 4.243M9.878 9.878l4.242 4.242M9.88 9.88l-3.29-3.29m7.532 7.532l3.29 3.29M3 3l3.59 3.59m0 0A9.953 9.953 0 0112 5c4.478 0 8.268 2.943 9.543 7a10.025 10.025 0 01-4.132 5.411m0 0L21 21" />
                      </svg>
                    ) : (
                      <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 12a3 3 0 11-6 0 3 3 0 016 0z" />
                        <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M2.458 12C3.732 7.943 7.523 5 12 5c4.478 0 8.268 2.943 9.542 7-1.274 4.057-5.064 7-9.542 7-4.477 0-8.268-2.943-9.542-7z" />
                      </svg>
                    )}
                  </button>
                </div>
              </div>

              {/* Error message */}
              {error && (
                <div className="rounded-md bg-red-50 dark:bg-red-900/30 border border-red-200 dark:border-red-700 px-3 py-2 text-sm text-red-700 dark:text-red-300" role="alert">
                  {error}
                </div>
              )}

              {/* Submit */}
              <button
                type="submit"
                disabled={loading}
                className="w-full py-2 px-4 bg-bodh-800 dark:bg-bodh-500 text-white text-sm font-medium rounded-md hover:bg-bodh-700 dark:hover:bg-bodh-400 focus:outline-none focus:ring-2 focus:ring-bodh-400 disabled:opacity-50 disabled:cursor-not-allowed transition-colors"
              >
                {loading ? t('login.submitting') : t('login.submit')}
              </button>
            </form>
          </div>
        </div>

        {/* Disclaimer */}
        <p className="text-center text-xs text-bodh-400 dark:text-gray-600 mt-6">
          {t('login.disclaimer')}
        </p>
      </div>
    </div>
  );
}
