import { Network, ShieldCheck, Database, ArrowRight } from "lucide-react";
import { ReactNode } from "react";

interface DifferenceCardProps {
  icon: ReactNode;
  title: string;
  description: string;
  callout: string;
  variant: "ember" | "frost" | "spark";
  number: string;
}

const DifferenceCard = ({ icon, title, description, callout, variant, number }: DifferenceCardProps) => {
  const getVariantStyles = () => {
    switch (variant) {
      case "ember":
        return {
          border: "border-glow-ember",
          gradient: "from-ember to-ember-glow",
          calloutColor: "text-ember",
          shadowColor: "shadow-ember/20",
          glowColor: "group-hover:shadow-ember/30",
        };
      case "frost":
        return {
          border: "border-glow-frost",
          gradient: "from-frost to-frost-glow",
          calloutColor: "text-frost",
          shadowColor: "shadow-frost/20",
          glowColor: "group-hover:shadow-frost/30",
        };
      case "spark":
        return {
          border: "",
          gradient: "from-spark to-spark-glow",
          calloutColor: "text-spark",
          shadowColor: "shadow-spark/20",
          glowColor: "group-hover:shadow-spark/30",
        };
    }
  };

  const styles = getVariantStyles();

  return (
    <div className={`group relative bento-card rounded-xl p-4 md:p-5 ${styles.border} flex flex-col h-full overflow-hidden transition-all duration-300 hover:-translate-y-1 ${styles.glowColor}`}>
      {/* Number watermark */}
      <div className="absolute -top-1 -right-1 text-5xl md:text-6xl font-black text-foreground/[0.03] select-none">
        {number}
      </div>
      
      {/* Gradient line at top */}
      <div className={`absolute top-0 left-0 right-0 h-[2px] bg-gradient-to-r ${styles.gradient} opacity-60`} />
      
      <div className="relative flex-1 flex flex-col">
        <div className={`w-9 h-9 md:w-10 md:h-10 rounded-xl bg-gradient-to-br ${styles.gradient} flex items-center justify-center mb-3 shadow-md ${styles.shadowColor}`}>
          {icon}
        </div>
        
        <h3 className="text-sm md:text-base font-bold text-foreground mb-1.5">{title}</h3>
        <p className="text-[11px] md:text-xs text-muted-foreground leading-relaxed flex-grow mb-3">{description}</p>
        
        <div className={`inline-flex items-center gap-1.5 text-[10px] md:text-xs font-semibold uppercase tracking-wider ${styles.calloutColor}`}>
          {callout}
          <ArrowRight className="w-3 h-3 opacity-0 -translate-x-2 group-hover:opacity-100 group-hover:translate-x-0 transition-all duration-300" />
        </div>
      </div>
    </div>
  );
};

const SovereignDifference = () => {
  const cards: DifferenceCardProps[] = [
    {
      icon: <Network className="w-4 h-4 md:w-5 md:h-5 text-primary-foreground" />,
      title: "Decentralized Architecture",
      description: "Distribute security across multiple nodes. No central target means no single point of compromise.",
      callout: "No Single Point of Failure",
      variant: "frost",
      number: "01",
    },
    {
      icon: <ShieldCheck className="w-4 h-4 md:w-5 md:h-5 text-primary-foreground" />,
      title: "Consensus-Verified Decisions",
      description: "Every threat decision validated by distributed consensus. Cryptographically verified and tamper-proof.",
      callout: "Verifiable Trust",
      variant: "ember",
      number: "02",
    },
    {
      icon: <Database className="w-4 h-4 md:w-5 md:h-5 text-primary-foreground" />,
      title: "Immutable Security Logs",
      description: "Your security logs belong to you. Immutable records that no third party can alter or delete.",
      callout: "Data Sovereignty",
      variant: "spark",
      number: "03",
    },
  ];

  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-2 md:gap-3">
      {cards.map((card, index) => (
        <div 
          key={card.title} 
          className="animate-reveal-up"
          style={{ animationDelay: `${index * 100}ms` }}
        >
          <DifferenceCard {...card} />
        </div>
      ))}
    </div>
  );
};

export default SovereignDifference;
