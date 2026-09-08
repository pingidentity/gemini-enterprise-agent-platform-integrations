import { login } from '../auth/oidc';

export default function LoginScreen() {
  return (
    <div className="min-h-screen flex flex-col bg-bg-primary text-white">
      <div className="flex-1 flex flex-col items-center justify-center text-center gap-6 p-16 md:p-8 animate-[slide-up_0.6s_ease-out]">
        <div className="text-[5rem] text-ping-red mb-4 animate-[dot-pulse_3s_ease-in-out_infinite] drop-shadow-[0_0_20px_rgba(227,24,55,0.3)]">🛒</div>
        <h2 className="text-4xl md:text-2xl font-bold uppercase tracking-wide font-mono">Agent Chaining</h2>
        <p className="text-base text-text-secondary font-mono uppercase tracking-wide">Sign in to check your order through a secure agent chain</p>
        <button
          className="px-10 py-4 text-base bg-ping-red text-white font-bold cursor-pointer hover:shadow-[0_0_20px_rgba(227,24,55,0.4)] active:scale-[0.98] transition-all duration-200"
          onClick={login}
        >
          Sign In
        </button>
      </div>
    </div>
  );
}
