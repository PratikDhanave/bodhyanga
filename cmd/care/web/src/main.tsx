import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import { BrowserRouter } from 'react-router-dom';
import App from './App';
import I18nProvider from './i18n/I18nProvider';
import { ToastProvider } from './components/Toast';
import './index.css';

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <I18nProvider>
      <ToastProvider>
        <BrowserRouter basename="/ui">
          <App />
        </BrowserRouter>
      </ToastProvider>
    </I18nProvider>
  </StrictMode>,
);
