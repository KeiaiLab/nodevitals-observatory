import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
import App from '@/App';
import '@/index.css';

const rootEl = document.getElementById('root');
if (!rootEl) {
  throw new Error('observatory: #root 마운트 지점이 없다');
}

createRoot(rootEl).render(
  <StrictMode>
    <App />
  </StrictMode>,
);
