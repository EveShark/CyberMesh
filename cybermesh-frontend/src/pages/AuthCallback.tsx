import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "@/lib/auth/AuthProvider";

const RETURN_TO_STORAGE_KEY = "cybermesh:return_to";

export default function AuthCallback() {
  const navigate = useNavigate();
  const [error, setError] = useState<string>("");
  const { completeHostedLogin } = useAuth();

  useEffect(() => {
    let cancelled = false;

    const run = async () => {
      try {
        await completeHostedLogin();
        const returnTo = sessionStorage.getItem(RETURN_TO_STORAGE_KEY);
        if (returnTo) {
          sessionStorage.removeItem(RETURN_TO_STORAGE_KEY);
        }
        const target = returnTo && returnTo.startsWith("/") ? returnTo : "/dashboard";
        if (!cancelled) {
          navigate(target, { replace: true });
        }
      } catch (err) {
        sessionStorage.removeItem(RETURN_TO_STORAGE_KEY);
        if (!cancelled) {
          setError(err instanceof Error ? err.message : "Authentication callback failed");
        }
      }
    };

    run();
    return () => {
      cancelled = true;
    };
  }, [completeHostedLogin, navigate]);

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center px-6">
      <div className="w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900/80 p-8 shadow-2xl">
        <h1 className="text-2xl font-semibold">Signing you in</h1>
        <p className="mt-3 text-sm text-slate-400">
          Finalizing the hosted login and validating the backend session.
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
