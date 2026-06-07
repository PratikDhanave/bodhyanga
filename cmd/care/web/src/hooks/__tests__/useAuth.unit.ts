import { describe, it, expect, beforeEach } from 'vitest';
import { getToken, setToken, clearToken, isAuthenticated, getCurrentUser } from '../useAuth';

beforeEach(() => {
  localStorage.clear();
});

describe('useAuth token functions', () => {
  it('getToken returns null when no token is stored', () => {
    expect(getToken()).toBeNull();
  });

  it('setToken stores token in localStorage', () => {
    setToken('abc123');
    expect(localStorage.getItem('bodh_jwt')).toBe('abc123');
  });

  it('getToken retrieves stored token', () => {
    setToken('my-token');
    expect(getToken()).toBe('my-token');
  });

  it('clearToken removes token from localStorage', () => {
    setToken('my-token');
    clearToken();
    expect(getToken()).toBeNull();
  });

  it('isAuthenticated returns false when no token', () => {
    expect(isAuthenticated()).toBe(false);
  });

  it('isAuthenticated returns true when token exists', () => {
    setToken('any-token');
    expect(isAuthenticated()).toBe(true);
  });
});

describe('getCurrentUser', () => {
  it('returns null when no token is stored', () => {
    expect(getCurrentUser()).toBeNull();
  });

  it('parses JWT payload to extract email and role', () => {
    // JWT: { "email": "dr@hospital.org", "role": "clinician" }
    const payload = btoa(JSON.stringify({ email: 'dr@hospital.org', role: 'clinician' }));
    const token = `header.${payload}.signature`;
    setToken(token);

    const user = getCurrentUser();
    expect(user).toEqual({ email: 'dr@hospital.org', role: 'clinician' });
  });

  it('uses sub field as email when email is not present', () => {
    const payload = btoa(JSON.stringify({ sub: 'dr@example.com' }));
    const token = `header.${payload}.signature`;
    setToken(token);

    const user = getCurrentUser();
    expect(user?.email).toBe('dr@example.com');
  });

  it('defaults role to "user" when not present', () => {
    const payload = btoa(JSON.stringify({ email: 'test@test.com' }));
    const token = `header.${payload}.signature`;
    setToken(token);

    const user = getCurrentUser();
    expect(user?.role).toBe('user');
  });

  it('returns null for malformed token (wrong segment count)', () => {
    setToken('not-a-jwt');
    expect(getCurrentUser()).toBeNull();
  });

  it('returns null for invalid base64 payload', () => {
    setToken('header.!!!invalid!!!.signature');
    expect(getCurrentUser()).toBeNull();
  });

  it('returns null for non-JSON payload', () => {
    const payload = btoa('not json');
    setToken(`header.${payload}.signature`);
    expect(getCurrentUser()).toBeNull();
  });

  it('handles base64url encoding (- and _ characters)', () => {
    // Use base64url-safe characters
    const payload = btoa(JSON.stringify({ email: 'test@test.com', role: 'admin' }))
      .replace(/\+/g, '-')
      .replace(/\//g, '_')
      .replace(/=+$/, '');
    const token = `header.${payload}.signature`;
    setToken(token);

    const user = getCurrentUser();
    expect(user?.email).toBe('test@test.com');
  });
});
