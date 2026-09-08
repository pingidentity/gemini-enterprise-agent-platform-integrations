interface Props {
  onClose: () => void;
}

export default function ArchitectureModal({ onClose }: Props) {
  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/80 backdrop-blur-sm p-4"
      onClick={onClose}
    >
      <div
        className="relative bg-bg-secondary border-2 border-border max-w-3xl w-full max-h-[90vh] overflow-y-auto"
        onClick={(e) => e.stopPropagation()}
      >
        {/* Header */}
        <div className="sticky top-0 bg-bg-secondary border-b-2 border-border px-6 py-4 flex justify-between items-center">
          <span className="font-mono text-xs font-bold tracking-widest uppercase text-ping-red">
            Architecture
          </span>
          <button
            className="font-mono text-xs text-text-secondary hover:text-white transition-colors"
            onClick={onClose}
          >
            [ESC]
          </button>
        </div>

        <div className="p-6 flex flex-col gap-3">
          {/* Flow diagram */}
          <Flow />

          {/* Token chain */}
          <div className="mt-4 border border-border p-4 font-mono text-xs space-y-2">
            <div className="text-ping-red font-bold uppercase tracking-widest mb-3">Token Chain (RFC 8693)</div>
            <TokenChainRow label="user_token" claims="sub=alice  aud=bridge-resource  scope=stripe_mcp:invoke" color="text-emerald-400" />
            <div className="text-text-secondary pl-4">↓ RFC 8693 exchange (agent credentials as actor)</div>
            <TokenChainRow label="delegated_token" claims="sub=alice  act.sub=agent-client-id  scope=stripe_mcp:invoke" color="text-sky-400" />
            <div className="text-text-secondary pl-4">↓ RFC 8693 exchange (ext_proc IDP credentials as actor)</div>
            <TokenChainRow label="tool_token" claims="sub=alice  act chain  aud=stripe-mcp-server  scope=stripe_mcp:invoke" color="text-amber-400" />
          </div>
        </div>
      </div>
    </div>
  );
}

function TokenChainRow({ label, claims, color }: { label: string; claims: string; color: string }) {
  return (
    <div className="pl-4 border-l-2 border-border">
      <span className={`${color} font-bold`}>{label}</span>
      <span className="text-text-secondary ml-2">{claims}</span>
    </div>
  );
}

interface FlowNode {
  id: string;
  label: string;
  sub?: string;
  accent?: boolean;
}

interface FlowEdge {
  label: string;
  note?: string;
}

const NODES: FlowNode[] = [
  { id: 'ui',      label: 'Chat UI',             sub: 'PKCE login',                accent: false },
  { id: 'bridge',  label: 'Agent Bridge',         sub: 'Cloud Run',                 accent: false },
  { id: 'agent',   label: 'ADK Agent',            sub: 'Reasoning Engine',          accent: false },
  { id: 'gateway', label: 'Agent Gateway',        sub: 'Google-managed + ext_proc', accent: true  },
  { id: 'mcp',     label: 'Stripe MCP Server',    sub: 'Cloud Run',                 accent: false },
  { id: 'stripe',  label: 'Stripe API',           sub: 'external',                  accent: false },
];

const EDGES: FlowEdge[] = [
  { label: 'POST /chat',      note: 'Authorization: Bearer user_token' },
  { label: 'stream_query',    note: 'user_token in session state' },
  { label: 'MCP calls',       note: 'delegated_token (RFC 8693)' },
  { label: 'ext_proc → tool', note: 'tool_token + X-User-Email' },
  { label: 'tools/call',      note: 'tool_token + X-User-Email' },
];

function Flow() {
  return (
    <div className="flex flex-col items-center gap-0">
      {NODES.map((node, i) => (
        <div key={node.id} className="flex flex-col items-center w-full max-w-sm">
          <div
            className={`w-full border-2 px-4 py-3 text-center ${
              node.accent
                ? 'border-ping-red bg-[rgba(227,24,55,0.08)]'
                : 'border-border bg-bg-tertiary'
            }`}
          >
            <div className={`font-mono text-sm font-bold ${node.accent ? 'text-ping-red' : 'text-white'}`}>
              {node.label}
            </div>
            {node.sub && (
              <div className="font-mono text-[0.65rem] text-text-secondary mt-0.5 uppercase tracking-wide">
                {node.sub}
              </div>
            )}
          </div>
          {i < EDGES.length && (
            <div className="flex flex-col items-center py-1">
              <div className="w-px h-2 bg-border" />
              <div className="font-mono text-[0.65rem] text-center px-2">
                <div className="text-ping-red font-bold">{EDGES[i].label}</div>
                {EDGES[i].note && <div className="text-text-secondary">{EDGES[i].note}</div>}
              </div>
              <div className="w-px h-2 bg-border" />
              <div className="w-0 h-0 border-l-[5px] border-r-[5px] border-t-[6px] border-l-transparent border-r-transparent border-t-border" />
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
