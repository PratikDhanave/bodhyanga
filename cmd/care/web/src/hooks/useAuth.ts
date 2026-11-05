import { useNavigate } from 'react-router-dom';

const TOKEN_KEY = 'bodhyanga_jwt';

export interface JWTUser {
  email: string;
  role: string;
}

export function getToken(): string | null {
  return localStorage.getItem(TOKEN_KEY);
}

export function setToken(t: string): void {
  localStorage.setItem(TOKEN_KEY, t);
}

export function clearToken(): void {
  localStorage.removeItem(TOKEN_KEY);
}

export function isAuthenticated(): boolean {
  return getToken() !== null;
}

export function getCurrentUser(): JWTUser | null {
  const token = getToken();
  if (!token) return null;
  try {
    const parts = token.split('.');
    if (parts.length !== 3) return null;
    // Base64url → base64 → decode
    const payload = parts[1].replace(/-/g, '+').replace(/_/g, '/');
    const padded = payload + '='.repeat((4 - (payload.length % 4)) % 4);
    const json = atob(padded);
    const data = JSON.parse(json) as Record<string, unknown>;
    const email = typeof data.email === 'string' ? data.email :
                  typeof data.sub === 'string' ? data.sub : '';
    const role = typeof data.role === 'string' ? data.role : 'user';
    return { email, role };
  } catch {
    return null;
  }
}

export function useLogout(): () => void {
  const navigate = useNavigate();
  return () => {
    clearToken();
    navigate('/login');
  };
}
