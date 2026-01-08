import { Zap, TrendingUp, Shield, AlertOctagon } from "lucide-react";

const ThreatLandscape = () => {
  const points = [
    {
      icon: <Shield className="w-3 h-3 md:w-3.5 md:h-3.5" />,
      text: "NIST 2.0 & DORA require verifiable security",
      color: "text-frost",
      bg: "bg-frost/10 group-hover:bg-frost/20",
    },
    {
      icon: <Zap className="w-3 h-3 md:w-3.5 md:h-3.5" />,
      text: "AI attacks outpace human defenders 5,400×",
      color: "text-ember",
      bg: "bg-ember/10 group-hover:bg-ember/20",
    },
    {
      icon: <TrendingUp className="w-3 h-3 md:w-3.5 md:h-3.5" />,
      text: "Sovereign Cloud market growing 24% YoY",
      color: "text-spark",
      bg: "bg-spark/10 group-hover:bg-spark/20",
    },
    {
      icon: <AlertOctagon className="w-3 h-3 md:w-3.5 md:h-3.5" />,
      text: "Vendor consolidation creating systemic risk",
      color: "text-crimson",
      bg: "bg-crimson/10 group-hover:bg-crimson/20",
    },
  ];

  return (
    <div className="relative max-w-2xl mx-auto">
      {/* Decorative gradient orbs */}
      <div className="absolute -top-10 -left-10 w-24 h-24 bg-ember/10 rounded-full blur-3xl" />
      <div className="absolute -bottom-10 -right-10 w-24 h-24 bg-spark/10 rounded-full blur-3xl" />
      
      <div className="relative bento-card rounded-xl p-4 md:p-6 overflow-hidden">
        {/* Gradient border effect */}
        <div className="absolute inset-0 rounded-xl p-[1px] bg-gradient-to-br from-ember/40 via-transparent to-spark/40 opacity-50" />
        
        <div className="relative text-center mb-4 md:mb-6">
          <div className="inline-flex items-center justify-center w-12 h-12 md:w-14 md:h-14 rounded-xl gradient-ember mb-3 md:mb-4 animate-float shadow-lg ember-glow-sm">
            <svg className="w-6 h-6 md:w-7 md:h-7 text-primary-foreground" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M3.055 11H5a2 2 0 012 2v1a2 2 0 002 2 2 2 0 012 2v2.945M8 3.935V5.5A2.5 2.5 0 0010.5 8h.5a2 2 0 012 2 2 2 0 104 0 2 2 0 012-2h1.064M15 20.488V18a2 2 0 012-2h3.064M21 12a9 9 0 11-18 0 9 9 0 0118 0z" />
            </svg>
          </div>
          
          <h3 className="text-lg md:text-xl font-bold text-foreground mb-2 leading-tight">
            The Threat Landscape
            <br />
            <span className="text-gradient-ember">Has Fundamentally Changed</span>
          </h3>
          
          <p className="text-[11px] md:text-xs text-muted-foreground max-w-sm mx-auto leading-relaxed">
            Agentic AI attacks move at machine speed. Traditional security cannot keep pace.
          </p>
        </div>
        
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
          {points.map((point, index) => (
            <div 
              key={index} 
              className="group flex items-start gap-2.5 p-2.5 md:p-3 rounded-lg bg-secondary/30 hover:bg-secondary/50 border border-border/30 hover:border-ember/20 transition-all duration-300"
            >
              <div className={`w-6 h-6 md:w-7 md:h-7 rounded-lg ${point.bg} flex items-center justify-center flex-shrink-0 ${point.color} transition-colors duration-300`}>
                {point.icon}
              </div>
              <span className="text-[10px] md:text-xs text-foreground/80 leading-relaxed pt-0.5">{point.text}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
};

export default ThreatLandscape;
