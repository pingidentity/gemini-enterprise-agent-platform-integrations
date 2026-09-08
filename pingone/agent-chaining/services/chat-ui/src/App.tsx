import { useState, useEffect } from 'react';
import { handleCallback, isAuthenticated } from './auth/oidc';
import LoginScreen from './components/LoginScreen';
import ChatScreen from './components/ChatScreen';

/**
 * App entry point — handles the OIDC callback on page load, then routes
 * to LoginScreen or ChatScreen based on authentication state.
 */
export default function App() {
  const [authed, setAuthed] = useState(false);
  const [initializing, setInitializing] = useState(true);

  useEffect(() => {
    async function init() {
      // If AIC redirected back with an authorization code, exchange it for tokens
      if (window.location.search.includes('code=')) {
        await handleCallback();
      }
      setAuthed(isAuthenticated());
      setInitializing(false);
    }
    init();
  }, []);

  if (initializing) return null;

  return authed ? <ChatScreen /> : <LoginScreen />;
}
