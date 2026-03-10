import { Activity } from "lucide-react";

const DashboardHeader = () => {
  return (
    <header className="relative overflow-hidden rounded-2xl border border-border/70 bg-card/95 shadow-sm mb-6">
      <div className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-accent/50 to-transparent" />
      <div className="relative px-6 py-7 md:px-8">
        <div className="flex items-center gap-3">
          <div className="p-3 rounded-xl bg-accent/10 border border-accent/25">
            <Activity className="w-5 h-5 text-primary" />
          </div>
          <div>
            <h1 className="text-3xl md:text-4xl font-display font-bold tracking-tight text-primary">
              CyberMesh <span className="text-accent">Overview</span>
            </h1>
            <p className="text-muted-foreground max-w-2xl mt-1">
              Unified operating system for autonomous cybersecurity defense, telemetry, and infrastructure monitoring.
            </p>
          </div>
        </div>
      </div>
    </header>
  );
};

export default DashboardHeader;
