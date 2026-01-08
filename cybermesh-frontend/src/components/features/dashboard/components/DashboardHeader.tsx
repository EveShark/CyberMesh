import { Activity } from "lucide-react";

const DashboardHeader = () => {
  return (
    <header className="relative overflow-hidden">
      {/* Background mesh effect */}
      <div className="absolute inset-0 bg-gradient-to-br from-frost/5 via-transparent to-fire/5" />
      <div className="absolute inset-0 opacity-30">
        <svg className="w-full h-full" xmlns="http://www.w3.org/2000/svg">
          <defs>
            <pattern id="grid" width="40" height="40" patternUnits="userSpaceOnUse">
              <path d="M 40 0 L 0 0 0 40" fill="none" stroke="hsl(var(--primary) / 0.1)" strokeWidth="1" />
            </pattern>
          </defs>
          <rect width="100%" height="100%" fill="url(#grid)" />
        </svg>
      </div>

      <div className="relative container mx-auto px-4 py-8">
        <div className="flex items-center justify-between">
          <div className="space-y-2">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg glass-frost frost-glow">
                <Activity className="w-6 h-6 text-frost" />
              </div>
              <h1 className="text-3xl md:text-4xl font-bold text-foreground">
                CyberMesh <span className="text-gradient">Overview</span>
              </h1>
            </div>
            <p className="text-muted-foreground max-w-xl">
              Unified operating system for autonomous cybersecurity defense. Real-time telemetry, threat intelligence, and infrastructure monitoring.
            </p>
          </div>
        </div>
      </div>
    </header>
  );
};

export default DashboardHeader;
