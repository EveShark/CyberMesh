import { useLocation, Link } from "react-router-dom";
import { useEffect } from "react";
import { Helmet } from "react-helmet-async";
import { Home, LayoutDashboard, AlertTriangle } from "lucide-react";
import { Button } from "@/components/ui/button";
import Logo from "@/components/brand/Logo";

const NotFound = () => {
  const location = useLocation();

  // Check if this is a generic 404 (redirected from catch-all)
  const isGeneric404 = location.pathname === '/404';

  useEffect(() => {
    // Log 404 error to console for monitoring
    console.warn("404 Error: User attempted to access non-existent route:", {
      path: location.pathname,
      referrer: document.referrer || null,
      timestamp: new Date().toISOString(),
    });
  }, [location.pathname]);

  return (
    <>
      <Helmet>
        <title>404 - Page Not Found | CyberMesh</title>
        <meta name="description" content="The page you're looking for doesn't exist." />
        <meta name="robots" content="noindex, nofollow" />
      </Helmet>
      <div className="min-h-screen bg-background flex flex-col items-center justify-center px-4 relative overflow-hidden">
        {/* Background effects */}
        <div className="absolute inset-0 bg-gradient-to-br from-ember/5 via-background to-frost/5 pointer-events-none" />
        <div className="absolute top-1/4 left-1/4 w-96 h-96 bg-ember/10 rounded-full blur-3xl pointer-events-none" />
        <div className="absolute bottom-1/4 right-1/4 w-96 h-96 bg-frost/10 rounded-full blur-3xl pointer-events-none" />
        
        <div className="relative z-10 text-center max-w-md mx-auto">
          {/* Logo */}
          <div className="mb-8 flex justify-center">
            <Logo size={48} />
          </div>
          
          {/* 404 Display */}
          <div className="mb-6">
            <div className="flex items-center justify-center gap-3 mb-4">
              <AlertTriangle className="w-8 h-8 text-ember animate-pulse" />
              <h1 className="text-6xl md:text-7xl font-black text-gradient-ember">404</h1>
            </div>
            <h2 className="text-xl md:text-2xl font-bold text-foreground mb-3">
              Page Not Found
            </h2>
            <p className="text-sm md:text-base text-muted-foreground leading-relaxed">
              {isGeneric404 
                ? "The page you're looking for doesn't exist in our network."
                : <>The path <code className="px-2 py-1 bg-muted rounded text-xs font-mono text-ember">{location.pathname}</code> doesn't exist in our network.</>
              }
            </p>
          </div>
          
          {/* Action buttons */}
          <div className="flex flex-col sm:flex-row items-center justify-center gap-3">
            <Button asChild className="gradient-ember text-primary-foreground font-semibold hover:scale-105 transition-transform ember-glow-sm">
              <Link to="/">
                <Home className="w-4 h-4 mr-2" />
                Return Home
              </Link>
            </Button>
            <Button asChild variant="outline" className="border-border hover:border-frost/40 hover:bg-frost/5 transition-colors">
              <Link to="/dashboard">
                <LayoutDashboard className="w-4 h-4 mr-2" />
                Go to Dashboard
              </Link>
            </Button>
          </div>
          
          {/* Subtle network reference */}
          <p className="mt-6 text-xs text-muted-foreground/60">
            Network integrity verified • All systems operational
          </p>
        </div>
      </div>
    </>
  );
};

export default NotFound;
