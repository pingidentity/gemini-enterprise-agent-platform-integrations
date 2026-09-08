/**
 * OIDC Authentication (PKCE)
 *
 * Implements the Authorization Code flow with PKCE against PingOne AIC.
 * This is a public client (no client secret) — the code verifier/challenge
 * prevents authorization code interception.
 *
 * The resulting access token includes a `may_act` claim that authorizes
 * Support Agent to perform token exchange (RFC 8693) and act on behalf
 * of the user.
 */

interface TokenResponse {
  access_token: string;
  id_token?: string;
  token_type: string;
  expires_in: number;
}

interface IdTokenClaims {
  sub: string;
  email?: string;
  name?: string;
  [key: string]: unknown;
}

const AIC_ISSUER = import.meta.env.VITE_AIC_ISSUER as string;
const CLIENT_ID = import.meta.env.VITE_CLIENT_ID as string;
const REDIRECT_URI = import.meta.env.VITE_REDIRECT_URI as string;
const SCOPES = (import.meta.env.VITE_SCOPES as string) || 'openid profile email support-agent:invoke';

const STORAGE_KEYS = {
  accessToken: 'oidc_access_token',
  idToken: 'oidc_id_token',
  codeVerifier: 'oidc_code_verifier',
  expiresAt: 'oidc_expires_at',
} as const;

function base64UrlEncode(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  for (const b of bytes) binary += String.fromCharCode(b);
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

function generateRandomString(length: number): string {
  const array = new Uint8Array(length);
  crypto.getRandomValues(array);
  return base64UrlEncode(array.buffer);
}

async function generateCodeChallenge(verifier: string): Promise<string> {
  const encoder = new TextEncoder();
  const digest = await crypto.subtle.digest('SHA-256', encoder.encode(verifier));
  return base64UrlEncode(digest);
}

function parseJwt(token: string): IdTokenClaims {
  const base64Url = token.split('.')[1];
  const base64 = base64Url.replace(/-/g, '+').replace(/_/g, '/');
  return JSON.parse(atob(base64));
}

/** Redirect to PingOne AIC authorization endpoint with PKCE challenge. */
export async function login(): Promise<void> {
  const verifier = generateRandomString(64);
  sessionStorage.setItem(STORAGE_KEYS.codeVerifier, verifier);
  const challenge = await generateCodeChallenge(verifier);

  const params = new URLSearchParams({
    response_type: 'code',
    client_id: CLIENT_ID,
    redirect_uri: REDIRECT_URI,
    scope: SCOPES,
    code_challenge: challenge,
    code_challenge_method: 'S256',
    prompt: 'login',
  });

  window.location.href = `${AIC_ISSUER}/authorize?${params}`;
}

/** Exchange the authorization code for tokens after AIC redirects back. */
export async function handleCallback(): Promise<boolean> {
  const params = new URLSearchParams(window.location.search);
  const code = params.get('code');
  if (!code) return false;

  const verifier = sessionStorage.getItem(STORAGE_KEYS.codeVerifier);
  if (!verifier) return false;

  const body = new URLSearchParams({
    grant_type: 'authorization_code',
    code,
    redirect_uri: REDIRECT_URI,
    client_id: CLIENT_ID,
    code_verifier: verifier,
  });

  const res = await fetch(`${AIC_ISSUER}/token`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
    body,
  });

  if (!res.ok) {
    console.error('Token exchange failed:', await res.text());
    return false;
  }

  const token: TokenResponse = await res.json();
  sessionStorage.setItem(STORAGE_KEYS.accessToken, token.access_token);
  if (token.id_token) sessionStorage.setItem(STORAGE_KEYS.idToken, token.id_token);
  sessionStorage.setItem(STORAGE_KEYS.expiresAt, String(Date.now() + token.expires_in * 1000));
  sessionStorage.removeItem(STORAGE_KEYS.codeVerifier);

  // Clean URL — redirect back to root
  window.history.replaceState({}, '', '/');
  return true;
}

export function isAuthenticated(): boolean {
  const token = sessionStorage.getItem(STORAGE_KEYS.accessToken);
  const expiresAt = sessionStorage.getItem(STORAGE_KEYS.expiresAt);
  if (!token || !expiresAt) return false;
  return Date.now() < Number(expiresAt);
}

export function getAccessToken(): string | null {
  if (!isAuthenticated()) return null;
  return sessionStorage.getItem(STORAGE_KEYS.accessToken);
}

export function getUserInfo(): IdTokenClaims | null {
  const idToken = sessionStorage.getItem(STORAGE_KEYS.idToken);
  if (!idToken) return null;
  try {
    return parseJwt(idToken);
  } catch {
    return null;
  }
}

export function logout(): void {
  const idToken = sessionStorage.getItem(STORAGE_KEYS.idToken);
  Object.values(STORAGE_KEYS).forEach((k) => sessionStorage.removeItem(k));
  const postLogoutUri = encodeURIComponent(window.location.origin);
  window.location.href = `${AIC_ISSUER}/signoff?id_token_hint=${idToken ?? ''}&post_logout_redirect_uri=${postLogoutUri}`;
}
