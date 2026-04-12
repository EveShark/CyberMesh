import { Navigate, Outlet, useLocation } from "react-router-dom";
import { useAuth } from "@/lib/auth/AuthProvider";
import { ROUTES } from "@/config/routes";

export default function ProtectedRoute() {
  const location = useLocation();
  const { authEnabled, isLoading, session } = useAuth();

  if (!authEnabled) {
    return <Outlet />;
  }

  if (isLoading) {
    return (
      <div className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center px-6">
        <div className="w-full max-w-md rounded-2xl border border-slate-800 bg-slate-900/80 p-8 shadow-2xl">
          <h1 className="text-2xl font-semibold">Loading session</h1>
          <p className="mt-3 text-sm text-slate-400">
            Validating your hosted login and access context.
          </p>
        </div>
      </div>
    );
  }

  if (!session?.authenticated) {
    return <Navigate to={ROUTES.AUTH_LOGIN} replace state={{ from: location }} />;
  }

  return <Outlet />;
}
