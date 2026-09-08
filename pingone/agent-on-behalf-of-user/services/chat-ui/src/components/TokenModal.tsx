import { useState } from 'react';
import { getAccessToken } from '../auth/oidc';

interface Props {
  onClose: () => void;
}

function decodeJwt(token: string): Record<string, unknown> | null {
  try {
    const payload = token.split('.')[1];
    return JSON.parse(atob(payload.replace(/-/g, '+').replace(/_/g, '/')));
  } catch {
    return null;
  }
}

function formatClaim(value: unknown): string {
  if (typeof value === 'number' && value > 1_000_000_000) {
    // Unix timestamp
    return `${value} (${new Date(value * 1000).toUTCString()})`;
  }
  if (typeof value === 'object') {
    return JSON.stringify(value, null, 2);
  }
  return String(value);
}

function ClaimsTable({ claims }: { claims: Record<string, unknown> }) {
  const order = ['sub', 'iss', 'aud', 'scope', 'act', 'iat', 'exp', 'client_id'];
  const keys = [
    ...order.filter((k) => k in claims),
    ...Object.keys(claims).filter((k) => !order.includes(k)),
  ];
  return (
    <table className="w-full text-xs font-mono border-collapse">
      <tbody>
        {keys.map((k) => (
          <tr key={k} className="border-b border-border/50">
            <td className="py-1.5 pr-4 text-ping-red font-bold align-top whitespace-nowrap w-28">{k}</td>
            <td className="py-1.5 text-white break-all whitespace-pre-wrap">{formatClaim(claims[k])}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

interface TokenSection {
  label: string;
  hop: string;
  color: string;
  token: string | null;
  example?: boolean;
  claims: Record<string, unknown> | null;
}

function buildExampleDelegated(userClaims: Record<string, unknown> | null): Record<string, unknown> {
  const now = Math.floor(Date.now() / 1000);
  return {
    sub: userClaims?.sub ?? 'user-sub-from-user-token',
    iss: userClaims?.iss ?? 'https://auth.pingone.../as',
    aud: 'stripe-mcp-server',
    scope: 'stripe_mcp:invoke',
    act: { sub: 'agent-client-id' },
    iat: now,
    exp: now + 300,
  };
}

function buildExampleTool(userClaims: Record<string, unknown> | null): Record<string, unknown> {
  const now = Math.floor(Date.now() / 1000);
  return {
    sub: userClaims?.sub ?? 'user-sub-from-user-token',
    iss: userClaims?.iss ?? 'https://auth.pingone.../as',
    aud: 'stripe-mcp-server',
    scope: 'stripe_mcp:invoke',
    act: { sub: 'ext-svc-client-id', act: { sub: 'agent-client-id' } },
    iat: now,
    exp: now + 300,
  };
}

export default function TokenModal({ onClose }: Props) {
  const rawToken = getAccessToken();
  const userClaims = rawToken ? decodeJwt(rawToken) : null;

  const sections: TokenSection[] = [
    {
      label: 'User Token',
      hop: 'Chat UI → Agent Bridge → ADK Agent',
      color: 'text-emerald-400',
      token: rawToken,
      claims: userClaims,
    },
    {
      label: 'Delegated Token',
      hop: 'ADK Agent → Agent Gateway',
      color: 'text-sky-400',
      token: null,
      example: true,
      claims: buildExampleDelegated(userClaims),
    },
    {
      label: 'Tool Token',
      hop: 'Agent Gateway → Stripe MCP Server',
      color: 'text-amber-400',
      token: null,
      example: true,
      claims: buildExampleTool(userClaims),
    },
  ];

  const [expanded, setExpanded] = useState<number>(0);
  const [copied, setCopied] = useState<number | null>(null);

  function copy(i: number, val: string) {
    navigator.clipboard.writeText(val);
    setCopied(i);
    setTimeout(() => setCopied(null), 1500);
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm p-4"
      onClick={onClose}
    >
      <div
        className="relative bg-bg-secondary border-2 border-border w-full max-w-2xl max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="sticky top-0 bg-bg-secondary border-b-2 border-border px-6 py-4 flex justify-between items-center">
          <span className="font-mono text-xs font-bold tracking-widest uppercase text-ping-red">
            Token Chain
          </span>
          <button
            className="font-mono text-xs text-text-secondary hover:text-white transition-colors"
            onClick={onClose}
          >
            [ESC]
          </button>
        </div>

        <div className="p-4 flex flex-col gap-3">
          {sections.map((s, i) => (
            <div key={i} className="border border-border">
              {/* Section header */}
              <button
                className="w-full flex justify-between items-center px-4 py-3 hover:bg-bg-tertiary transition-colors text-left"
                onClick={() => setExpanded(expanded === i ? -1 : i)}
              >
                <div className="flex items-center gap-3">
                  <span className={`font-mono text-xs font-bold ${s.color}`}>{s.label}</span>
                  {s.example && (
                    <span className="font-mono text-[0.6rem] text-text-secondary border border-border px-1.5 py-0.5 uppercase tracking-wide">
                      example
                    </span>
                  )}
                </div>
                <div className="flex items-center gap-3">
                  <span className="font-mono text-[0.65rem] text-text-secondary hidden sm:block">{s.hop}</span>
                  <span className="font-mono text-xs text-text-secondary">{expanded === i ? '▲' : '▼'}</span>
                </div>
              </button>

              {expanded === i && (
                <div className="border-t border-border px-4 py-3 bg-bg-primary">
                  {/* Hop label (mobile) */}
                  <div className="font-mono text-[0.65rem] text-text-secondary mb-3 sm:hidden">{s.hop}</div>

                  {/* Raw token or placeholder */}
                  {s.token ? (
                    <div className="mb-3 flex items-start gap-2">
                      <code className="flex-1 text-[0.6rem] font-mono text-text-secondary break-all leading-relaxed line-clamp-3">
                        {s.token}
                      </code>
                      <button
                        className="shrink-0 font-mono text-[0.6rem] text-text-secondary border border-border px-2 py-1 hover:text-white hover:border-ping-red transition-colors"
                        onClick={() => copy(i, s.token!)}
                      >
                        {copied === i ? 'copied!' : 'copy'}
                      </button>
                    </div>
                  ) : (
                    <div className="mb-3 font-mono text-[0.65rem] text-text-secondary italic">
                      Minted server-side — not visible to the browser
                    </div>
                  )}

                  {/* Claims */}
                  {s.claims ? (
                    <ClaimsTable claims={s.claims} />
                  ) : (
                    <div className="font-mono text-[0.65rem] text-text-secondary">Could not decode token</div>
                  )}
                </div>
              )}
            </div>
          ))}

          <p className="font-mono text-[0.65rem] text-text-secondary px-1">
            Delegated and tool tokens are minted server-side. Their structure above reflects
            the expected shape — sub, act chain, and aud are derived from your live user token.
          </p>
        </div>
      </div>
    </div>
  );
}
