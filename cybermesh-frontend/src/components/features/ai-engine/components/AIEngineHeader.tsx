import { Brain } from "lucide-react";

const AIEngineHeader = () => {
  return (
    <header className="relative overflow-hidden rounded-2xl border border-border/70 bg-card/95 shadow-sm p-6 md:p-8 mb-6">
      <div className="absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-accent/50 to-transparent" />
      <div className="relative z-10 flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div className="flex items-center gap-4">
          <div className="p-3 rounded-xl bg-accent/10 border border-accent/25">
            <Brain className="w-6 h-6 text-primary" />
          </div>
          <div>
            <h1 className="text-3xl md:text-4xl font-display font-bold tracking-tight text-primary">
              CyberMesh <span className="text-accent">AI Engine</span>
            </h1>
            <p className="text-muted-foreground max-w-xl mt-1">
              Real-time instrumentation for detection loop, variant pipelines, and suspicious validator signals.
            </p>
          </div>
        </div>

        {/* Live Refresh Indicator */}
        <div className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-status-healthy/10 border border-status-healthy/30">
          <span className="relative flex h-2.5 w-2.5">
            <span className="absolute inline-flex h-full w-full rounded-full bg-status-healthy/35"></span>
            <span className="relative inline-flex rounded-full h-2.5 w-2.5 bg-status-healthy"></span>
          </span>
          <span className="text-sm font-medium text-status-healthy">Live</span>
        </div>
      </div>
    </header>
  );
};

export default AIEngineHeader;
