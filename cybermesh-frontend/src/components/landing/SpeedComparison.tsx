import { useEffect, useRef, useState } from "react";
import { Zap, User, Check } from "lucide-react";

const SpeedComparison = () => {
  const [isVisible, setIsVisible] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const observer = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setIsVisible(true);
          observer.disconnect();
        }
      },
      { threshold: 0.3 }
    );

    if (containerRef.current) {
      observer.observe(containerRef.current);
    }

    return () => observer.disconnect();
  }, []);

  const aiMilestones = [
    { label: "Recon", position: 20 },
    { label: "Exploit", position: 45 },
    { label: "Exfil", position: 70 },
    { label: "Done", position: 95 },
  ];

  const humanMilestones = [
    { label: "Alert", position: 10 },
    { label: "Triage", position: 35 },
    { label: "Review...", position: 60 },
  ];

  return (
    <div ref={containerRef} className="w-full max-w-3xl mx-auto">
      {/* Compact horizontal timeline card */}
      <div className="bento-card rounded-xl p-4 md:p-5 space-y-4">
        {/* AI Attack Timeline */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <div className="w-7 h-7 md:w-8 md:h-8 rounded-lg gradient-ember flex items-center justify-center ember-glow-sm">
                <Zap className="w-3.5 h-3.5 md:w-4 md:h-4 text-primary-foreground" />
              </div>
              <span className="text-xs md:text-sm font-semibold text-foreground">AI Attack</span>
            </div>
            <div className="flex items-center gap-1.5">
              <span className="text-lg md:text-xl font-black text-[hsl(var(--status-healthy))]">~50ms</span>
              {isVisible && (
                <div className="w-5 h-5 rounded-full bg-[hsl(var(--status-healthy))]/20 flex items-center justify-center">
                  <Check className="w-3 h-3 text-[hsl(var(--status-healthy))]" />
                </div>
              )}
            </div>
          </div>
          
          {/* Timeline bar */}
          <div className="relative h-6 md:h-7 rounded-full bg-secondary/40 overflow-hidden">
            <div 
              className={`absolute inset-y-0 left-0 rounded-full bg-gradient-to-r from-[hsl(var(--status-healthy))] via-[hsl(var(--status-healthy-light))] to-[hsl(var(--status-healthy))] ${isVisible ? 'animate-fill-bar-fast' : 'w-0'}`}
              style={{ boxShadow: '0 0 15px hsl(var(--status-healthy) / 0.4)' }}
            />
            
            {/* Milestones */}
            <div className="absolute inset-0 flex items-center">
              {aiMilestones.map((milestone, i) => (
                <div
                  key={milestone.label}
                  className={`absolute flex flex-col items-center transition-all duration-300 ${
                    isVisible ? 'opacity-100' : 'opacity-0'
                  }`}
                  style={{ 
                    left: `${milestone.position}%`, 
                    transform: 'translateX(-50%)',
                    transitionDelay: `${i * 150 + 600}ms` 
                  }}
                >
                  <span className={`text-[9px] md:text-[10px] font-medium ${
                    i === aiMilestones.length - 1 ? 'text-primary-foreground' : 'text-foreground/90'
                  }`}>
                    {milestone.label}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>

        {/* Divider */}
        <div className="accent-line-ember" />

        {/* Human Response Timeline */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <div className="flex items-center gap-2">
              <div className="w-7 h-7 md:w-8 md:h-8 rounded-lg bg-muted flex items-center justify-center">
                <User className="w-3.5 h-3.5 md:w-4 md:h-4 text-muted-foreground" />
              </div>
              <span className="text-xs md:text-sm font-semibold text-foreground">Human Response</span>
            </div>
            <span className="text-lg md:text-xl font-black text-muted-foreground">~4.5 hrs</span>
          </div>
          
          {/* Timeline bar */}
          <div className="relative h-6 md:h-7 rounded-full bg-secondary/40 overflow-hidden">
            <div 
              className={`absolute inset-y-0 left-0 rounded-full bg-gradient-to-r from-muted-foreground/30 to-muted-foreground/50 ${isVisible ? 'animate-fill-bar-slow' : 'w-0'}`}
            />
            
            {/* Milestones */}
            <div className="absolute inset-0 flex items-center">
              {humanMilestones.map((milestone, i) => (
                <div
                  key={milestone.label}
                  className={`absolute flex flex-col items-center transition-all duration-500 ${
                    isVisible ? 'opacity-100' : 'opacity-0'
                  }`}
                  style={{ 
                    left: `${milestone.position}%`, 
                    transform: 'translateX(-50%)',
                    transitionDelay: `${i * 400 + 500}ms` 
                  }}
                >
                  <span className="text-[9px] md:text-[10px] font-medium text-muted-foreground">
                    {milestone.label}
                  </span>
                </div>
              ))}
            </div>
          </div>
        </div>
      </div>

      {/* Speed gap callout */}
      <div 
        className={`mt-3 md:mt-4 relative overflow-hidden rounded-xl p-3 md:p-4 transition-all duration-500 ${
          isVisible ? 'opacity-100 translate-y-0' : 'opacity-0 translate-y-4'
        }`}
        style={{ 
          transitionDelay: '1.5s',
          background: 'linear-gradient(135deg, hsl(var(--ember) / 0.12), hsl(var(--spark) / 0.08))',
          border: '1px solid hsl(var(--ember) / 0.25)',
        }}
      >
        <div className="absolute inset-0 bg-gradient-to-r from-transparent via-ember/5 to-transparent animate-gradient-x" />
        
        <div className="relative flex items-center justify-center gap-3 md:gap-4">
          <span className="text-2xl md:text-3xl lg:text-4xl font-black text-ember animate-glow-pulse">
            5,400×
          </span>
          <div className="h-8 w-px bg-border/50" />
          <p className="text-xs md:text-sm text-foreground/80 max-w-xs">
            The speed gap is <span className="text-ember font-semibold">insurmountable</span> with human-only defense
          </p>
        </div>
      </div>
    </div>
  );
};

export default SpeedComparison;
