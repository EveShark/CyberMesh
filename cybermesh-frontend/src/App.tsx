import { Toaster } from "@/components/ui/sonner";
import { TooltipProvider } from "@/components/ui/tooltip";
import { QueryClientProvider } from "@tanstack/react-query";
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom";
import { HelmetProvider } from "react-helmet-async";
import Index from "./pages/Index";
import Dashboard from "./pages/Dashboard";
import Delegations from "./pages/Delegations";
import AIEngine from "./pages/AIEngine";
import SystemHealth from "./pages/SystemHealth";
import Threats from "./pages/Threats";
import Blockchain from "./pages/Blockchain";
import Network from "./pages/Network";
import Settings from "./pages/Settings";
import AuthLogin from "./pages/AuthLogin";
import AuthCallback from "./pages/AuthCallback";
import AuthUnauthorized from "./pages/AuthUnauthorized";
import AuthSessionExpired from "./pages/AuthSessionExpired";
import { DashboardLayout } from "@/components/layout";
import NotFound from "./pages/NotFound";
import { GlobalErrorBoundary } from "@/components/ui/global-error-boundary";
import { queryClient } from "@/lib/query-client";
import { AuthProvider } from "@/lib/auth/AuthProvider";
import ProtectedRoute from "@/components/auth/ProtectedRoute";

const App = () => (
  <GlobalErrorBoundary>
    <HelmetProvider>
      <QueryClientProvider client={queryClient}>
        <TooltipProvider>
          <AuthProvider>
            <Toaster />
            <BrowserRouter>
              <Routes>
                <Route path="/" element={<Index />} />
                <Route path="/auth/login" element={<AuthLogin />} />
                <Route path="/auth/callback" element={<AuthCallback />} />
                <Route path="/auth/unauthorized" element={<AuthUnauthorized />} />
                <Route path="/auth/session-expired" element={<AuthSessionExpired />} />
                <Route element={<ProtectedRoute />}>
                  <Route element={<DashboardLayout />}>
                    <Route path="/dashboard" element={<Dashboard />} />
                    <Route path="/delegations" element={<Delegations />} />
                    <Route path="/ai-engine" element={<AIEngine />} />
                    <Route path="/threats" element={<Threats />} />
                    <Route path="/blockchain" element={<Blockchain />} />
                    <Route path="/network" element={<Network />} />
                    <Route path="/system-health" element={<SystemHealth />} />
                    <Route path="/settings" element={<Settings />} />
                  </Route>
                </Route>
                {/* ADD ALL CUSTOM ROUTES ABOVE THE CATCH-ALL "*" ROUTE */}
                <Route path="/404" element={<NotFound />} />
                <Route path="*" element={<Navigate to="/404" replace />} />
              </Routes>
            </BrowserRouter>
          </AuthProvider>
        </TooltipProvider>
      </QueryClientProvider>
    </HelmetProvider>
  </GlobalErrorBoundary>
);

export default App;
