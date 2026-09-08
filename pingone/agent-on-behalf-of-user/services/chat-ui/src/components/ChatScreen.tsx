import { useState, useEffect, useRef, useCallback } from 'react';
import { logout, getAccessToken, getUserInfo } from '../auth/oidc';
import { invokeAgent } from '../api/agent';
import ArchitectureModal from './ArchitectureModal';
import TokenModal from './TokenModal';
interface ChatMessage {
  id: string;
  role: 'user' | 'agent' | 'error';
  content: string;
  timestamp: Date;
}

export default function ChatScreen() {
  const [messages, setMessages] = useState<ChatMessage[]>([]);
  const [input, setInput] = useState('');
  const [loading, setLoading] = useState(false);
  const [showArch, setShowArch] = useState(false);
  const [showTokens, setShowTokens] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const userInfo = getUserInfo();
  const displayName = userInfo?.email ?? userInfo?.name ?? 'User';

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [messages]);

  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') { setShowArch(false); setShowTokens(false); }
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, []);

  const addMessage = useCallback((role: ChatMessage['role'], content: string) => {
    setMessages((prev) => [
      ...prev,
      { id: crypto.randomUUID(), role, content, timestamp: new Date() },
    ]);
  }, []);

  const handleSend = async (e: React.FormEvent) => {
    e.preventDefault();
    const text = input.trim();
    if (!text || loading) return;

    setInput('');
    addMessage('user', text);
    setLoading(true);

    try {
      const token = getAccessToken();
      if (!token) {
        addMessage('error', 'Session expired. Please log in again.');
        return;
      }
      const reply = await invokeAgent(text, token);
      addMessage('agent', reply);
    } catch (err) {
      addMessage('error', err instanceof Error ? err.message : 'Unknown error');
    } finally {
      setLoading(false);
    }
  };

  const formatTime = (d: Date) =>
    d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' });

  const msgBase = 'flex flex-col gap-2 p-6 md:p-4 border-2 bg-bg-secondary relative animate-[slide-up_0.3s_ease-out_both] before:absolute before:top-0 before:left-0 before:w-1 before:h-full before:bg-ping-red before:scale-y-0 before:origin-top before:transition-transform before:duration-300 hover:before:scale-y-100';

  const btnStyle = 'px-5 py-2.5 bg-transparent text-text-secondary border-2 border-border font-mono text-xs font-bold uppercase tracking-widest cursor-pointer transition-all duration-200 hover:border-ping-red hover:text-white hover:bg-ping-red';

  return (
    <div className="min-h-screen flex flex-col bg-bg-primary text-white relative">
      {showArch && <ArchitectureModal onClose={() => setShowArch(false)} />}
      {showTokens && <TokenModal onClose={() => setShowTokens(false)} />}
      {/* Header */}
      <header className="px-4 py-6 md:p-4 border-b-2 border-border bg-bg-secondary sticky top-0 z-50">
        <div className="max-w-[1200px] mx-auto flex justify-between items-center">
          <div className="font-mono text-2xl md:text-xl font-bold tracking-widest uppercase flex items-center">
            <span className="text-ping-red font-bold">[</span>
            <span className="text-white mx-1 font-bold">OBO FINANCIAL AGENT</span>
            <span className="text-ping-red font-bold">]</span>
          </div>
          <div className="flex items-center gap-2">
            <span className="font-mono text-xs text-text-secondary hidden sm:block">{displayName}</span>
            <button className={btnStyle} onClick={() => setShowArch(true)} title="Architecture">Arch</button>
            <button className={btnStyle} onClick={() => setShowTokens(true)} title="Token chain">Tokens</button>
            <button className={btnStyle} onClick={() => setMessages([])}>Clear</button>
            <button className={btnStyle} onClick={logout}>Logout</button>
          </div>
        </div>
      </header>

      {/* Messages */}
      <main className="flex-1 overflow-hidden flex flex-col">
        <div className="flex-1 max-w-[1200px] w-full mx-auto p-8 md:p-4 overflow-y-auto flex flex-col">
          {messages.length === 0 ? (
            <div className="flex-1 flex flex-col items-center justify-center text-center gap-6 py-16 px-8 animate-[slide-up_0.6s_ease-out]">
              <div className="text-[5rem] text-ping-red mb-4 animate-[dot-pulse_3s_ease-in-out_infinite] drop-shadow-[0_0_20px_rgba(227,24,55,0.3)]">🛒</div>
              <h2 className="text-4xl font-bold uppercase tracking-wide font-mono">How can I help?</h2>
              <p className="text-base text-text-secondary font-mono uppercase tracking-wide">Ask about products, check your account, or make a purchase — acting on your behalf</p>
            </div>
          ) : (
            <div className="flex flex-col gap-6 py-4">
              {messages.map((m) => (
                <div
                  key={m.id}
                  className={`${msgBase} ${
                    m.role === 'user'
                      ? 'border-ping-red bg-gradient-to-br from-bg-secondary to-[rgba(227,24,55,0.05)]'
                      : m.role === 'error'
                        ? 'border-error bg-[rgba(255,68,68,0.05)]'
                        : 'border-border'
                  }`}
                >
                  <div className="flex justify-between items-center mb-2">
                    <span className={`font-mono text-[0.7rem] font-semibold tracking-widest uppercase ${m.role === 'error' ? 'text-error' : 'text-ping-red'}`}>
                      {m.role === 'user' ? 'You' : m.role === 'agent' ? 'Agent' : 'Error'}
                    </span>
                    <span className="font-mono text-[0.7rem] text-text-secondary">{formatTime(m.timestamp)}</span>
                  </div>
                  <div className="text-[0.95rem] leading-relaxed text-white break-words whitespace-pre-wrap">{m.content}</div>
                </div>
              ))}
              {loading && (
                <div className={`${msgBase} border-ping-red bg-[rgba(0,255,136,0.03)]`}>
                  <div className="flex justify-between items-center mb-2">
                    <span className="font-mono text-[0.7rem] font-semibold tracking-widest uppercase text-ping-red">Agent</span>
                  </div>
                  <div className="flex gap-2 py-2">
                    <span className="w-2 h-2 rounded-full bg-ping-red animate-[dot-pulse_1.4s_ease-in-out_infinite]" />
                    <span className="w-2 h-2 rounded-full bg-ping-red animate-[dot-pulse_1.4s_ease-in-out_0.2s_infinite]" />
                    <span className="w-2 h-2 rounded-full bg-ping-red animate-[dot-pulse_1.4s_ease-in-out_0.4s_infinite]" />
                  </div>
                </div>
              )}
              <div ref={messagesEndRef} />
            </div>
          )}
        </div>
      </main>

      {/* Input */}
      <footer className="px-4 py-6 pb-8 md:p-4 border-t-2 border-border bg-bg-secondary relative before:absolute before:top-[-2px] before:left-0 before:right-0 before:h-[2px] before:bg-gradient-to-r before:from-transparent before:via-ping-red before:to-transparent before:opacity-50">
        <form className="max-w-[1200px] mx-auto mb-3" onSubmit={handleSend}>
          <div className="flex gap-3 p-3 bg-bg-tertiary border-2 border-border transition-all duration-200 focus-within:border-ping-red focus-within:shadow-[0_0_0_2px_rgba(227,24,55,0.15)]">
            <input
              className="flex-1 bg-transparent border-none outline-none text-white font-display text-[0.95rem] p-2 placeholder:text-text-secondary disabled:opacity-50 disabled:cursor-not-allowed"
              type="text"
              placeholder="Type a message..."
              value={input}
              onChange={(e) => setInput(e.target.value)}
              disabled={loading}
              autoFocus
            />
            <button
              className="px-5 py-3 bg-ping-red text-white font-bold cursor-pointer flex items-center justify-center transition-all duration-200 hover:not-disabled:shadow-[0_0_20px_rgba(227,24,55,0.4)] active:not-disabled:scale-[0.98] disabled:opacity-30 disabled:cursor-not-allowed"
              type="submit"
              disabled={loading || !input.trim()}
            >
              <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <line x1="22" y1="2" x2="11" y2="13" />
                <polygon points="22 2 15 22 11 13 2 9 22 2" />
              </svg>
            </button>
          </div>
        </form>
        <div className="max-w-[1200px] mx-auto font-mono text-[0.7rem] text-text-secondary text-center flex items-center justify-center gap-2 uppercase tracking-wide">
          <span className="w-1.5 h-1.5 rounded-full bg-ping-red animate-[dot-pulse_2s_ease-in-out_infinite]" />
          <span>Powered by PingOne AIC, PingAuthorize, Google ADK, Agent Gateway, and Stripe MCP</span>
        </div>
      </footer>
    </div>
  );
}
