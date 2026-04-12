import { ShieldAlert } from "lucide-react";
import { useSearchParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useAuth } from "@/lib/auth/AuthProvider";
import { ROUTES } from "@/config/routes";

export default function AuthUnauthorized() {
  const { startLogin, startLogout } = useAuth();
  const [searchParams] = useSearchParams();
  const returnToCandidate = searchParams.get("returnTo");
  const returnTo = returnToCandidate && returnToCandidate.startsWith("/")
    ? returnToCandidate
    : ROUTES.DASHBOARD;

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center px-6">
      <Card className="w-full max-w-lg border-slate-800 bg-slate-900/80 text-slate-100 shadow-2xl">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-2xl">
            <ShieldAlert className="h-6 w-6 text-amber-400" />
            Access denied
          </CardTitle>
          <CardDescription className="text-slate-400">
            Your session is valid, but this action is outside the access scope or role assigned to this principal.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-3">
          <Button onClick={() => void startLogin(returnTo)}>Try a different session</Button>
          <Button variant="outline" onClick={() => void startLogout()}>Sign out</Button>
        </CardContent>
      </Card>
    </div>
  );
}
