import { Zap, Shield, Eye, Lock } from "lucide-react";

const badges = [
  {
    icon: Zap,
    label: "Sub-Second",
    sublabel: "Detection",
  },
  {
    icon: Shield,
    label: "Autonomous",
    sublabel: "Defense",
  },
  {
    icon: Eye,
    label: "24/7",
    sublabel: "Monitoring",
  },
  {
    icon: Lock,
    label: "Zero Trust",
    sublabel: "Architecture",
  },
];

const TrustBadges = () => {
  return (
    <div className="flex flex-wrap items-center justify-center gap-4 md:gap-6 lg:gap-8 py-4">
      {badges.map((badge, index) => (
        <div 
          key={index}
          className="flex items-center gap-2 px-3 py-2 rounded-lg glass border border-border/30 hover:border-ember/30 transition-colors"
        >
          <badge.icon className="w-4 h-4 md:w-5 md:h-5 text-ember" />
          <div className="flex flex-col">
            <span className="text-[10px] md:text-xs font-semibold text-foreground leading-tight">
              {badge.label}
            </span>
            <span className="text-[9px] md:text-[10px] text-muted-foreground leading-tight">
              {badge.sublabel}
            </span>
          </div>
        </div>
      ))}
    </div>
  );
};

export default TrustBadges;