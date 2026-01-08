import { Outlet, useLocation, Link } from "react-router-dom";
import { useEffect, useRef } from "react";
import { SidebarProvider, SidebarTrigger, useSidebar } from "@/components/ui/sidebar";
import { AppSidebar } from "./AppSidebar";
import { MobileBottomNav } from "./MobileBottomNav";
import Breadcrumbs from "./Breadcrumbs";
import { OfflineBanner } from "@/components/ui/offline-banner";
import { DemoModeBanner } from "@/components/ui/demo-mode-banner";
import { ConnectionStatus } from "@/components/ui/connection-status";
import { RefreshControl } from "@/components/ui/refresh-control";
import { Menu, Shield, Settings, X } from "lucide-react";
import { useSwipe } from "@/hooks/ui/use-swipe";
import { useIsFetching, useQueryClient } from "@tanstack/react-query";
import { useConnectionStatus } from "@/hooks/common/use-connection-status";
import { isDemoMode } from "@/config/demo-mode";
import { ROUTES } from "@/config/routes";
import { cn } from "@/lib/utils";

const DashboardContent = () => {
  const isFetching = useIsFetching();
  const queryClient = useQueryClient();
  const { setOpenMobile, isMobile } = useSidebar();
  const location = useLocation();
  const mainRef = useRef<HTMLElement>(null);
  const { isOnline, lastSyncTimeFormatted, syncError, recordSuccessfulSync, recordSyncError } = useConnectionStatus();

  const isSettingsPage = location.pathname === ROUTES.SETTINGS;

  // Subscribe to query cache updates to track successful syncs (skip in demo mode)
  useEffect(() => {
    if (isDemoMode()) return;

    const unsubscribe = queryClient.getQueryCache().subscribe((event) => {
      if (event.type === "updated") {
        if (event.query.state.status === "success") {
          recordSuccessfulSync();
        } else if (event.query.state.status === "error") {
          recordSyncError();
        }
      }
    });

    return () => unsubscribe();
  }, [queryClient, recordSuccessfulSync, recordSyncError]);

  // Scroll to top when route changes
  useEffect(() => {
    // Scroll both window and main container for reliable scroll-to-top
    window.scrollTo(0, 0);
    mainRef.current?.scrollTo({ top: 0, behavior: "instant" });
  }, [location.pathname]);

  // Swipe gestures for mobile
  useSwipe({
    onSwipeRight: () => {
      if (isMobile) setOpenMobile(true);
    },
    onSwipeLeft: () => {
      if (isMobile) setOpenMobile(false);
    },
    threshold: 50,
    edgeWidth: 30,
  });

  return (
    <div className="min-h-[100dvh] flex w-full overflow-x-hidden">
      <AppSidebar />
      <div className="flex-1 flex flex-col min-w-0">
        {/* Header with hamburger menu and branding */}
        <header className="h-14 flex items-center border-b border-border/50 bg-background/95 backdrop-blur-xl px-4 sticky top-0 z-50 relative overflow-hidden">
          {/* Global loading indicator */}
          {isFetching > 0 && (
            <div className="absolute bottom-0 left-0 right-0 h-0.5 overflow-hidden">
              <div className="h-full w-full bg-gradient-to-r from-frost via-primary to-frost animate-loading-bar" />
            </div>
          )}
          {/* Hamburger menu with branding */}
          <div className="flex items-center gap-3">
            <SidebarTrigger className="h-10 w-10 flex items-center justify-center rounded-lg hover:bg-muted/50 transition-colors">
              <Menu className="h-5 w-5" />
            </SidebarTrigger>

            {/* Mobile branding - visible on mobile only */}
            <div className="flex md:hidden items-center gap-2">
              <div className="relative flex-shrink-0">
                <div className="absolute inset-0 rounded-md bg-gradient-to-br from-frost to-fire opacity-60 blur-sm" />
                <div className="relative w-7 h-7 rounded-md bg-gradient-to-br from-frost to-frost-glow flex items-center justify-center">
                  <Shield className="w-4 h-4 text-primary-foreground" />
                </div>
              </div>
              <span className="font-bold text-sm sidebar-logo-shimmer">CyberMesh</span>
            </div>
          </div>

          {/* Right side - Refresh, Connection status, Settings (mobile), and Design Partner badge */}
          <div className="flex items-center gap-2 md:gap-3 ml-auto md:ml-4">
            {/* Refresh control with last update time */}
            <RefreshControl lastSyncTimeFormatted={lastSyncTimeFormatted} />

            {/* Connection status indicator */}
            <ConnectionStatus
              isOnline={isOnline}
              lastSyncTimeFormatted={lastSyncTimeFormatted}
              syncError={syncError}
              isFetching={isFetching > 0}
            />

            {/* Mobile Settings shortcut - visible on mobile only */}
            <Link
              to={isSettingsPage ? ROUTES.DASHBOARD : ROUTES.SETTINGS}
              className={cn(
                "md:hidden h-9 w-9 flex items-center justify-center rounded-lg transition-colors border",
                isSettingsPage
                  ? "bg-secondary text-foreground border-border"
                  : "border-transparent hover:bg-muted/50 text-muted-foreground hover:text-foreground"
              )}
              aria-label={isSettingsPage ? "Close Settings" : "Settings"}
            >
              {isSettingsPage ? <X className="h-4 w-4" /> : <Settings className="h-4 w-4" />}
            </Link>

            {/* Design Partner Environment indicator */}
            <div className="hidden lg:flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-frost/10 border border-frost/20 backdrop-blur-sm">
              <span className="text-[10px] font-medium text-frost tracking-wide">Design Partner Environment</span>
            </div>
          </div>
        </header>

        {/* Offline banner */}
        <OfflineBanner />

        {/* Demo mode banner for VC pitches */}
        <DemoModeBanner />

        {/* Breadcrumb navigation */}
        <Breadcrumbs />

        {/* Main content - with bottom padding for mobile nav */}
        <main ref={mainRef} className="flex-1 overflow-y-auto overflow-x-hidden pb-20 md:pb-0">
          <Outlet />
        </main>
      </div>

      {/* Mobile Bottom Navigation */}
      <MobileBottomNav />
    </div>
  );
};

const DashboardLayout = () => {
  return (
    <SidebarProvider>
      <DashboardContent />
    </SidebarProvider>
  );
};

export default DashboardLayout;
