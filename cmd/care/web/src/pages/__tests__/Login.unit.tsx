import { describe, it, expect, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import Login from '../Login';
import I18nProvider from '../../i18n/I18nProvider';
import { mockFetch } from '../../test/setup';

function renderLogin() {
  return render(
    <I18nProvider>
      <MemoryRouter initialEntries={['/login']}>
        <Routes>
          <Route path="/login" element={<Login />} />
          <Route path="/" element={<div>Dashboard</div>} />
        </Routes>
      </MemoryRouter>
    </I18nProvider>,
  );
}

describe('Login', () => {
  it('renders the sign in form with email and password fields', () => {
    renderLogin();
    expect(screen.getByLabelText('Email')).toBeInTheDocument();
    expect(screen.getByLabelText('Password')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Sign in' })).toBeInTheDocument();
  });

  it('renders branding elements', () => {
    renderLogin();
    expect(screen.getByText('Bodh')).toBeInTheDocument();
    expect(screen.getByText('Clinical AI orchestration platform')).toBeInTheDocument();
    expect(screen.getByText(/Research demo/)).toBeInTheDocument();
  });

  it('toggles password visibility when show/hide button is clicked', async () => {
    renderLogin();
    const user = userEvent.setup();
    const passwordInput = screen.getByLabelText('Password');
    expect(passwordInput).toHaveAttribute('type', 'password');

    const toggleBtn = screen.getByLabelText('Show password');
    await user.click(toggleBtn);
    expect(passwordInput).toHaveAttribute('type', 'text');

    const hideBtn = screen.getByLabelText('Hide password');
    await user.click(hideBtn);
    expect(passwordInput).toHaveAttribute('type', 'password');
  });

  it('shows loading state during submission', async () => {
    // Make fetch never resolve during this test
    vi.stubGlobal('fetch', vi.fn(() => new Promise(() => {})));
    const user = userEvent.setup();
    renderLogin();

    await user.type(screen.getByLabelText('Email'), 'dr@hospital.org');
    await user.type(screen.getByLabelText('Password'), 'password123');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    expect(screen.getByRole('button', { name: 'Signing in…' })).toBeDisabled();
  });

  it('navigates to dashboard on successful login', async () => {
    mockFetch((input, init) => {
      const url = typeof input === 'string' ? input : input.toString();
      if (url.endsWith('/api/auth/login') && init?.method === 'POST') {
        return { token: 'eyJhbGciOiJIUzI1NiJ9.eyJlbWFpbCI6ImRyQGhvc3BpdGFsLm9yZyIsInJvbGUiOiJjbGluaWNpYW4ifQ.sig' };
      }
      return {};
    });
    const user = userEvent.setup();
    renderLogin();

    await user.type(screen.getByLabelText('Email'), 'dr@hospital.org');
    await user.type(screen.getByLabelText('Password'), 'password123');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    await waitFor(() => {
      expect(screen.getByText('Dashboard')).toBeInTheDocument();
    });
  });

  it('shows error when server returns non-OK status', async () => {
    vi.stubGlobal('fetch', vi.fn(async () =>
      new Response('Unauthorized', { status: 401 }),
    ));
    const user = userEvent.setup();
    renderLogin();

    await user.type(screen.getByLabelText('Email'), 'dr@hospital.org');
    await user.type(screen.getByLabelText('Password'), 'wrong');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    await waitFor(() => {
      expect(screen.getByText('Invalid credentials')).toBeInTheDocument();
    });
  });

  it('shows error when response has no token', async () => {
    mockFetch(() => ({})); // no token field
    const user = userEvent.setup();
    renderLogin();

    await user.type(screen.getByLabelText('Email'), 'dr@hospital.org');
    await user.type(screen.getByLabelText('Password'), 'pass');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    await waitFor(() => {
      expect(screen.getByText('Invalid credentials')).toBeInTheDocument();
    });
  });

  it('shows network error on fetch failure', async () => {
    vi.stubGlobal('fetch', vi.fn(async () => { throw new Error('Failed to fetch'); }));
    const user = userEvent.setup();
    renderLogin();

    await user.type(screen.getByLabelText('Email'), 'dr@hospital.org');
    await user.type(screen.getByLabelText('Password'), 'pass');
    await user.click(screen.getByRole('button', { name: 'Sign in' }));

    await waitFor(() => {
      expect(screen.getByText('Network error — please try again')).toBeInTheDocument();
    });
  });
});
