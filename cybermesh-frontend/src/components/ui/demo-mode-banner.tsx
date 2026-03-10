import { useState, useEffect, useCallback } from "react";
import { FlaskConical, ArrowRight, X, Sparkles } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Link } from "react-router-dom";
import { ROUTES } from "@/config/routes";
import { isDemoMode } from "@/config/demo-mode";
import { cn } from "@/lib/utils";

const DISMISS_KEY = "cybermesh-demo-banner-dismissed";
const DEMO_MODE_KEY = "cybermesh-demo-mode";

export const DemoModeBanner = () => {
  const [shouldShow, setShouldShow] = useState(false);
  const [isVisible, setIsVisible] = useState(false);

  // Check demo mode status and handle visibility
  const checkDemoModeStatus = useCallback(() => {
    const inDemoMode = isDemoMode();
    const dismissed = sessionStorage.getItem(DISMISS_KEY);
    const lastDemoModeState = sessionStorage.getItem("demo-banner-last-state");
    
    // If demo mode was toggled (changed state), reset dismissal
    if (lastDemoModeState !== null && lastDemoModeState !== String(inDemoMode)) {
      sessionStorage.removeItem(DISMISS_KEY);
    }
    
    // Track current demo mode state
    sessionStorage.setItem("demo-banner-last-state", String(inDemoMode));
    
    // Show banner if in demo mode and not dismissed
    if (inDemoMode && !sessionStorage.getItem(DISMISS_KEY)) {
      setShouldShow(true);
      setTimeout(() => setIsVisible(true), 100);
    } else {
      setIsVisible(false);
      setTimeout(() => setShouldShow(false), 300);
    }
  }, []);

  // Check on mount and listen for storage changes (demo mode toggle)
  useEffect(() => {
    checkDemoModeStatus();
    
    // Listen for localStorage changes (demo mode toggle from Settings)
    const handleStorageChange = (e: StorageEvent) => {
      if (e.key === DEMO_MODE_KEY) {
        checkDemoModeStatus();
      }
    };
    
    window.addEventListener("storage", handleStorageChange);
    
    // Also poll for changes (for same-tab updates)
    const interval = setInterval(checkDemoModeStatus, 1000);
    
    return () => {
      window.removeEventListener("storage", handleStorageChange);
      clearInterval(interval);
    };
  }, [checkDemoModeStatus]);

  const handleDismiss = () => {
    setIsVisible(false);
    setTimeout(() => {
      setShouldShow(false);
      sessionStorage.setItem(DISMISS_KEY, "true");
    }, 300);
  };

  if (!shouldShow) return null;

  return (
    <div
      className={cn(
        "relative overflow-hidden transition-all duration-500 ease-out",
        isVisible 
          ? "opacity-100 max-h-40 translate-y-0" 
          : "opacity-0 max-h-0 -translate-y-2"
      )}
    >
      <div className="relative mx-4 my-3 rounded-xl border border-accent/35 bg-card/95 shadow-sm">
        {/* Mobile Layout (stacked) */}
        <div className="flex flex-col gap-2.5 p-3 md:hidden">
          {/* Header row */}
          <div className="flex items-center justify-between gap-2">
            <div className="flex items-center gap-2">
              <div className="flex items-center justify-center w-8 h-8 rounded-lg bg-accent/15 border border-accent/25">
                <FlaskConical className="w-4 h-4 text-primary" />
              </div>
              <span className="font-semibold text-sm text-primary">Demo Mode</span>
            </div>
            <button
              onClick={handleDismiss}
              className="p-1.5 rounded-lg hover:bg-accent/10 transition-colors text-muted-foreground hover:text-foreground"
              aria-label="Dismiss banner"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
          
          {/* Message */}
          <p className="text-xs text-muted-foreground leading-relaxed">
            You're viewing demonstration data. Our production system provides{" "}
            <span className="text-primary font-medium">real-time blockchain telemetry</span>.
          </p>
          
          {/* CTA Button */}
          <Button
            asChild
            size="sm"
            className="w-full bg-primary text-primary-foreground hover:bg-primary/90"
          >
            <Link to={ROUTES.SETTINGS} className="flex items-center justify-center gap-2">
              <Sparkles className="w-4 h-4" />
              Enable Live Data
              <ArrowRight className="w-4 h-4" />
            </Link>
          </Button>
        </div>
        
        {/* Desktop Layout (inline) */}
        <div className="hidden md:flex items-center justify-between gap-4 px-4 py-3">
          <div className="flex items-center gap-4">
            <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-accent/15 border border-accent/25">
              <FlaskConical className="w-5 h-5 text-primary" />
            </div>
            <div className="flex flex-col sm:flex-row sm:items-center gap-1 sm:gap-3">
              <span className="font-semibold text-primary">Demo Mode</span>
              <span className="text-sm text-muted-foreground">
                Viewing sample data for demonstration.{" "}
                <span className="text-primary">Real-time blockchain telemetry</span> available.
              </span>
            </div>
          </div>
          
          <div className="flex items-center gap-2 shrink-0">
            <Button
              asChild
              size="sm"
              className="bg-primary text-primary-foreground hover:bg-primary/90"
            >
              <Link to={ROUTES.SETTINGS} className="gap-2">
                <Sparkles className="w-3.5 h-3.5" />
                Enable Live Data
                <ArrowRight className="w-3.5 h-3.5" />
              </Link>
            </Button>
            <button
              onClick={handleDismiss}
              className="p-2 rounded-lg hover:bg-accent/10 transition-colors text-muted-foreground hover:text-foreground"
              aria-label="Dismiss banner"
            >
              <X className="w-4 h-4" />
            </button>
          </div>
        </div>
      </div>
    </div>
  );
};

export default DemoModeBanner;
