import React, { useState } from 'react';
import { Mail, Lock, Shield, ArrowRight, KeyRound } from 'lucide-react';

export default function CyberMeshLogin() {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [show2FA, setShow2FA] = useState(false);
  const [twoFactorCode, setTwoFactorCode] = useState('');
  const [isLoading, setIsLoading] = useState(false);

  const handleLogin = (e) => {
    e.preventDefault();
    if (password && !show2FA) {
      setIsLoading(true);
      setTimeout(() => {
        setIsLoading(false);
        setShow2FA(true);
      }, 1000);
    }
  };

  return (
    <div className="min-h-screen bg-black relative overflow-hidden flex items-center justify-center p-4">
      {/* Subtle Grid Background */}
      <div className="absolute inset-0">
        <div className="absolute inset-0 bg-[linear-gradient(rgba(6,182,212,0.05)_1px,transparent_1px),linear-gradient(90deg,rgba(6,182,212,0.05)_1px,transparent_1px)] bg-[size:48px_48px]"></div>
        <div className="absolute top-0 left-1/4 w-96 h-96 bg-cyan-500/10 rounded-full blur-3xl"></div>
        <div className="absolute bottom-0 right-1/4 w-96 h-96 bg-cyan-500/10 rounded-full blur-3xl"></div>
      </div>

      {/* Login Card */}
      <div className="relative z-10 w-full max-w-md">
        {/* Logo and Title */}
        <div className="mb-8">
          <div className="flex items-center gap-3 mb-6">
            <div className="flex items-center justify-center w-10 h-10 rounded-lg bg-cyan-500/10 border border-cyan-500/30">
              <Shield className="w-5 h-5 text-cyan-400" />
            </div>
            <div>
              <h1 className="text-2xl font-semibold text-white tracking-tight">CyberMesh</h1>
              <p className="text-sm text-zinc-500">Security Portal</p>
            </div>
          </div>
        </div>

        {/* Login Form Card */}
        <div className="bg-zinc-900 rounded-lg border border-zinc-800 overflow-hidden shadow-2xl">
          <div className="p-8">
            <h2 className="text-lg font-medium text-white mb-6">Sign in to continue</h2>
            
            <form onSubmit={handleLogin} className="space-y-5">
              {/* Email Input */}
              <div className="space-y-2">
                <label htmlFor="email" className="text-sm font-medium text-zinc-300 block">
                  Email
                </label>
                <div className="relative group">
                  <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                    <Mail className="h-4 w-4 text-zinc-500 group-focus-within:text-cyan-400 transition-colors" />
                  </div>
                  <input
                    id="email"
                    type="email"
                    value={email}
                    onChange={(e) => setEmail(e.target.value)}
                    className="w-full pl-10 pr-4 py-2.5 bg-black border border-zinc-700 rounded-md text-white text-sm placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-cyan-500 focus:border-cyan-500 transition-all hover:border-zinc-600"
                    placeholder="you@company.com"
                    required
                  />
                </div>
              </div>

              {/* Password Input */}
              <div className="space-y-2">
                <label htmlFor="password" className="text-sm font-medium text-zinc-300 block">
                  Password
                </label>
                <div className="relative group">
                  <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                    <Lock className="h-4 w-4 text-zinc-500 group-focus-within:text-cyan-400 transition-colors" />
                  </div>
                  <input
                    id="password"
                    type="password"
                    value={password}
                    onChange={(e) => setPassword(e.target.value)}
                    className="w-full pl-10 pr-4 py-2.5 bg-black border border-zinc-700 rounded-md text-white text-sm placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-cyan-500 focus:border-cyan-500 transition-all hover:border-zinc-600"
                    placeholder="••••••••••"
                    required
                  />
                </div>
              </div>

              {/* 2FA Input - Appears after password entered */}
              {show2FA && (
                <div className="space-y-2 animate-slideDown">
                  <label htmlFor="2fa" className="text-sm font-medium text-zinc-300 block flex items-center gap-2">
                    <KeyRound className="h-4 w-4 text-cyan-400" />
                    Two-Factor Code
                  </label>
                  <div className="relative group">
                    <div className="absolute inset-y-0 left-0 pl-3 flex items-center pointer-events-none">
                      <Shield className="h-4 w-4 text-zinc-500 group-focus-within:text-cyan-400 transition-colors" />
                    </div>
                    <input
                      id="2fa"
                      type="text"
                      value={twoFactorCode}
                      onChange={(e) => setTwoFactorCode(e.target.value)}
                      className="w-full pl-10 pr-4 py-2.5 bg-black border border-zinc-700 rounded-md text-white text-sm placeholder-zinc-600 focus:outline-none focus:ring-1 focus:ring-cyan-500 focus:border-cyan-500 transition-all hover:border-zinc-600 tracking-wider"
                      placeholder="000000"
                      maxLength="6"
                      pattern="[0-9]{6}"
                    />
                  </div>
                  <p className="text-xs text-zinc-600 mt-1">Enter 6-digit code from authenticator app</p>
                </div>
              )}

              {/* Login Button */}
              <button
                type="submit"
                disabled={isLoading}
                className="w-full py-2.5 px-4 bg-cyan-500 hover:bg-cyan-400 text-black text-sm font-medium rounded-md transition-all focus:outline-none focus:ring-2 focus:ring-cyan-500 focus:ring-offset-2 focus:ring-offset-zinc-900 disabled:opacity-50 disabled:cursor-not-allowed flex items-center justify-center gap-2 group"
              >
                {isLoading ? (
                  <>
                    <svg className="animate-spin h-4 w-4 text-black" xmlns="http://www.w3.org/2000/svg" fill="none" viewBox="0 0 24 24">
                      <circle className="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="4"></circle>
                      <path className="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4zm2 5.291A7.962 7.962 0 014 12H0c0 3.042 1.135 5.824 3 7.938l3-2.647z"></path>
                    </svg>
                    <span>Signing in...</span>
                  </>
                ) : (
                  <>
                    <span>Sign in</span>
                    <ArrowRight className="h-4 w-4 group-hover:translate-x-0.5 transition-transform" />
                  </>
                )}
              </button>
            </form>
          </div>

          {/* Footer Links */}
          <div className="px-8 py-4 bg-black border-t border-zinc-800 flex items-center justify-between text-xs">
            <a
              href="#"
              className="text-zinc-500 hover:text-cyan-400 transition-colors"
            >
              Forgot password?
            </a>
            <a
              href="#"
              className="text-zinc-500 hover:text-cyan-400 transition-colors"
            >
              ← Back to site
            </a>
          </div>
        </div>

        {/* Security Notice */}
        <div className="mt-6 text-center">
          <p className="text-xs text-zinc-600 flex items-center justify-center gap-1.5">
            <Shield className="h-3 w-3" />
            <span>Secured with end-to-end encryption</span>
          </p>
        </div>
      </div>

      {/* Custom CSS for animations */}
      <style jsx>{`
        @keyframes slideDown {
          from {
            opacity: 0;
            transform: translateY(-8px);
          }
          to {
            opacity: 1;
            transform: translateY(0);
          }
        }

        .animate-slideDown {
          animation: slideDown 0.2s ease-out;
        }

        @import url('https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600&display=swap');

        * {
          font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', sans-serif;
        }
      `}</style>
    </div>
  );
}
