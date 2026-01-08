import { Brain } from "lucide-react";

const AIEngineHeader = () => {
  return (
    <header className="relative overflow-hidden rounded-2xl glass-frost border border-border/50 p-6 md:p-8 mb-6">
      {/* Animated mesh background */}
      <div className="absolute inset-0 opacity-20">
        <div className="absolute top-0 left-1/4 w-72 h-72 bg-primary/30 rounded-full blur-3xl animate-pulse" />
        <div className="absolute bottom-0 right-1/4 w-64 h-64 bg-accent/20 rounded-full blur-3xl animate-pulse delay-1000" />
      </div>

      <div className="relative z-10 flex flex-col md:flex-row md:items-center md:justify-between gap-4">
        <div className="flex items-center gap-4">
          <div className="p-3 rounded-lg glass-frost frost-glow">
            <Brain className="w-6 h-6 text-frost" />
          </div>
          <div>
            <h1 className="text-3xl md:text-4xl font-bold text-foreground">
              CyberMesh <span className="text-gradient">AI Engine</span>
            </h1>
            <p className="text-muted-foreground max-w-xl mt-1">
              Real-time instrumentation for detection loop, variant pipelines, and suspicious validator signals.
            </p>
          </div>
        </div>

        {/* Live Refresh Indicator */}
        <div className="flex items-center gap-2 px-3 py-1.5 rounded-full bg-emerald-500/10 border border-emerald-500/30">
          <span className="relative flex h-2.5 w-2.5">
            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
            <span className="relative inline-flex rounded-full h-2.5 w-2.5 bg-emerald-500"></span>
          </span>
          <span className="text-sm font-medium text-emerald-400">Live</span>
        </div>
      </div>
    </header>
  );
};

export default AIEngineHeader;
