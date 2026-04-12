import { useEffect, useState } from "react";
import { useLocation } from "react-router-dom";
import { useAuth } from "@/lib/auth/AuthProvider";

export default function AuthLogin() {
  const [error, setError] = useState<string>("");
  const location = useLocation();
  const { startLogin } = useAuth();

  useEffect(() => {
    const fromState = location.state as { from?: { pathname?: string; search?: string; hash?: string } } | null;
    const fromPath = fromState?.from?.pathname ? `${fromState.from.pathname}${fromState.from.search ?? ""}${fromState.from.hash ?? ""}` : undefined;
    void startLogin(fromPath).catch((err) => {
      setError(err instanceof Error ? err.message : "Failed to start hosted login");
    });
  }, [location.state, startLogin]);

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center px-6">
      <div className="w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900/80 p-8 shadow-2xl">
        <h1 className="text-2xl font-semibold">Redirecting to sign in</h1>
        <p className="mt-3 text-sm text-slate-400">
          Hosted authentication is handled by ZITADEL.
        </p>
        {error ? (
          <div className="mt-6 rounded-xl border border-red-900 bg-red-950/60 p-4 text-sm text-red-200">
            {error}
          </div>
        ) : (
          <div className="mt-6 text-sm text-slate-300">Please wait...</div>
        )}
      </div>
    </div>
  );
}
