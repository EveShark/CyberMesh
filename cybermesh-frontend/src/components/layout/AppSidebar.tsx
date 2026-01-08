import { Home } from "lucide-react";
import { NavLink } from "@/components/landing/NavLink";
import { useLocation } from "react-router-dom";
import { SIDEBAR_ITEMS } from "@/config/navigation";
import { ROUTES } from "@/config/routes";
import Logo from "@/components/brand/Logo";
import { useCallback } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { apiClient } from "@/lib/api";
import { useSystemHealthData } from "@/hooks/data/use-system-health-data";
import { isDemoMode } from "@/config/demo-mode";

import {
  Sidebar,
  SidebarContent,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarFooter,
  useSidebar,
} from "@/components/ui/sidebar";

// Map routes to their query keys and fetch functions for prefetching
const PREFETCH_CONFIG: Record<string, { queryKey: string[]; queryFn: () => Promise<unknown> }> = {
  [ROUTES.DASHBOARD]: {
    queryKey: ["dashboard-overview"],
    queryFn: () => apiClient.dashboard.getOverview(),
  },
  [ROUTES.AI_ENGINE]: {
    queryKey: ["ai-engine-status"],
    queryFn: () => apiClient.aiEngine.getStatus(),
  },
  [ROUTES.THREATS]: {
    queryKey: ["threats-summary"],
    queryFn: () => apiClient.threats.getSummary(),
  },
  [ROUTES.BLOCKCHAIN]: {
    queryKey: ["blockchain-data"],
    queryFn: () => apiClient.blockchain.getData(),
  },
  [ROUTES.NETWORK]: {
    queryKey: ["network-status"],
    queryFn: () => apiClient.network.getStatus(),
  },
  [ROUTES.SYSTEM_HEALTH]: {
    queryKey: ["system-health"],
    queryFn: () => apiClient.systemHealth.getStatus(),
  },
};

export function AppSidebar() {
  const { state, isMobile, setOpenMobile } = useSidebar();
  // On mobile, always show expanded content with labels
  const collapsed = isMobile ? false : state === "collapsed";
  const location = useLocation();
  const currentPath = location.pathname;
  const queryClient = useQueryClient();

  // Fetch system health for dynamic status display (skip polling in sidebar, use stale data)
  const { data: healthData } = useSystemHealthData({
    pollingInterval: 60000, // Poll less frequently in sidebar
    enabled: !isDemoMode() // Disable in demo mode
  });

  const demoMode = isDemoMode();

  const isActive = (path: string) => currentPath === path;

  // Close mobile sidebar on navigation
  const handleNavClick = () => {
    if (isMobile) {
      setOpenMobile(false);
    }
  };

  // Prefetch data on hover to reduce perceived latency
  const handleMouseEnter = useCallback(
    (path: string) => {
      const config = PREFETCH_CONFIG[path];
      if (config && !isActive(path)) {
        queryClient.prefetchQuery({
          queryKey: config.queryKey,
          queryFn: config.queryFn,
          staleTime: 10000, // Only prefetch if data is older than 10s
        });
      }
    },
    [queryClient, currentPath]
  );

  return (
    <Sidebar
      collapsible="icon"
      className="border-r border-border/30 sidebar-glass"
    >
      <SidebarContent className="flex flex-col">
        {/* Logo Header */}
        <div className="border-b border-border/30 p-4">
          <div className="flex items-center justify-center">
            <Logo
              size={collapsed ? 24 : 28}
              variant={collapsed ? "icon" : "full"}
              showText={!collapsed}
            />
          </div>
        </div>

        {/* Navigation */}
        <SidebarGroup className="flex-1 py-4">
          {!collapsed && (
            <SidebarGroupLabel className="text-[10px] text-muted-foreground uppercase tracking-widest font-semibold px-4 mb-2">
              Command Center
            </SidebarGroupLabel>
          )}
          <SidebarGroupContent>
            <SidebarMenu className={`space-y-1 ${collapsed ? 'px-0' : 'px-2'}`}>
              {SIDEBAR_ITEMS.map((item) => {
                const active = isActive(item.url);
                return (
                  <SidebarMenuItem key={item.title} className={collapsed ? 'flex justify-center' : ''}>
                    <SidebarMenuButton
                      asChild
                      isActive={active}
                      tooltip={item.title}
                      className={collapsed ? 'w-10 h-10 p-0' : 'p-0'}
                    >
                      <NavLink
                        to={item.url}
                        end
                        onClick={handleNavClick}
                        onMouseEnter={() => handleMouseEnter(item.url)}
                        className={`
                          sidebar-item flex items-center rounded-lg
                          text-muted-foreground
                          hover:text-foreground hover:bg-muted/50
                          group
                          ${collapsed ? 'justify-center w-10 h-10 p-0' : 'gap-3 px-3 py-2.5'}
                        `}
                        activeClassName="sidebar-item-active sidebar-item-glow text-foreground"
                      >
                        <div className={`
                          relative flex items-center justify-center rounded-lg
                          transition-all duration-200
                          ${collapsed ? 'w-10 h-10' : 'w-8 h-8'}
                          ${active
                            ? 'bg-gradient-to-br from-frost/20 to-frost/5'
                            : 'group-hover:bg-muted/80'
                          }
                        `}>
                          <item.icon className={`
                            w-4 h-4 flex-shrink-0 transition-all duration-200
                            ${active
                              ? 'text-frost drop-shadow-[0_0_6px_hsl(var(--frost)/0.5)]'
                              : 'group-hover:text-foreground'
                            }
                          `} />
                          {active && (
                            <div className="absolute inset-0 rounded-lg bg-frost/10 animate-pulse" />
                          )}
                        </div>
                        {!collapsed && (
                          <span className={`
                            text-sm font-medium transition-all duration-200
                            ${active ? 'text-foreground' : ''}
                          `}>
                            {item.title === 'Settings'
                              ? (demoMode ? 'Settings (Demo)' : 'Settings (Live)')
                              : item.title}
                          </span>
                        )}
                      </NavLink>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                );
              })}
            </SidebarMenu>
          </SidebarGroupContent>
        </SidebarGroup>

        {/* Divider */}
        <div className="mx-4 sidebar-divider" />

        {/* Quick Stats - Only when expanded and not in demo mode */}
        {!collapsed && !demoMode && (
          <div className="p-4">
            <div className="p-3 rounded-lg bg-gradient-to-br from-muted/50 to-muted/20 border border-border/30">
              <div className="flex items-center justify-between mb-2">
                <span className="text-[10px] uppercase tracking-wider text-muted-foreground font-medium">
                  System Status
                </span>
                <span className={`flex h-2 w-2 rounded-full animate-pulse ${healthData?.data?.serviceReadiness === "Ready"
                    ? "bg-status-healthy"
                    : healthData?.data?.serviceReadiness === "Degraded"
                      ? "bg-status-warning"
                      : "bg-status-critical"
                  }`} />
              </div>
              <div className="grid grid-cols-2 gap-2 text-xs">
                <div>
                  <span className="text-muted-foreground">Uptime</span>
                  <p className="font-semibold text-foreground">
                    {healthData?.data?.backendUptime?.runtime ?? "--"}
                  </p>
                </div>
                <div>
                  <span className="text-muted-foreground">Active</span>
                  <p className={`font-semibold ${healthData?.data?.serviceReadiness === "Ready"
                      ? "text-status-healthy"
                      : healthData?.data?.serviceReadiness === "Degraded"
                        ? "text-status-warning"
                        : "text-status-critical"
                    }`}>
                    {healthData?.data?.serviceReadiness === "Ready"
                      ? "Online"
                      : healthData?.data?.serviceReadiness === "Degraded"
                        ? "Degraded"
                        : healthData?.data?.serviceReadiness ?? "Checking..."}
                  </p>
                </div>
              </div>
            </div>
          </div>
        )}
      </SidebarContent>

      {/* Footer */}
      <SidebarFooter className={`border-t border-border/30 ${collapsed ? 'p-2 flex justify-center' : 'p-3'}`}>
        <SidebarMenuButton
          asChild
          tooltip="Exit Dashboard"
          className={collapsed ? 'w-10 h-10 p-0' : 'p-0'}
        >
          <NavLink
            to="/"
            onClick={handleNavClick}
            className={`sidebar-item flex items-center rounded-lg text-muted-foreground hover:text-fire hover:bg-fire/10 transition-all duration-200 group ${collapsed ? 'justify-center w-10 h-10 p-0' : 'gap-3 px-3 py-2.5'}`}
          >
            <div className={`flex items-center justify-center rounded-lg group-hover:bg-fire/20 transition-all duration-200 ${collapsed ? 'w-10 h-10' : 'w-8 h-8'}`}>
              <Home className="w-4 h-4 flex-shrink-0 transition-colors duration-200 group-hover:text-fire" />
            </div>
            {!collapsed && (
              <span className="text-sm font-medium">Exit Dashboard</span>
            )}
          </NavLink>
        </SidebarMenuButton>
      </SidebarFooter>
    </Sidebar>
  );
}

export default AppSidebar;
