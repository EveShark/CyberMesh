import { Clock3 } from "lucide-react";
import { useSearchParams } from "react-router-dom";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { useAuth } from "@/lib/auth/AuthProvider";
import { ROUTES } from "@/config/routes";

export default function AuthSessionExpired() {
  const { startLogin } = useAuth();
  const [searchParams] = useSearchParams();
  const returnTo = searchParams.get("returnTo") || ROUTES.DASHBOARD;

  return (
    <div className="min-h-screen bg-slate-950 text-slate-100 flex items-center justify-center px-6">
      <Card className="w-full max-w-lg border-slate-800 bg-slate-900/80 text-slate-100 shadow-2xl">
        <CardHeader>
          <CardTitle className="flex items-center gap-2 text-2xl">
            <Clock3 className="h-6 w-6 text-primary" />
            Session expired
          </CardTitle>
          <CardDescription className="text-slate-400">
            The hosted login is no longer valid. Sign in again to restore API access and access context.
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Button onClick={() => void startLogin(returnTo)}>Sign in again</Button>
        </CardContent>
      </Card>
    </div>
  );
}
