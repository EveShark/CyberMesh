import { AlertTriangle, DollarSign, Clock } from "lucide-react";
import { ReactNode } from "react";

interface StatCardProps {
  value: string;
  description: string;
  icon: ReactNode;
  gradient: string;
  delay: number;
}

const StatCard = ({ value, description, icon, gradient, delay }: StatCardProps) => (
  <div 
    className="group relative glass-card-modern glass-card-hover rounded-xl p-4 md:p-5"
    style={{ animationDelay: `${delay}ms` }}
  >
    {/* Gradient orb background */}
    <div 
      className={`absolute -top-8 -right-8 w-24 h-24 rounded-full blur-3xl opacity-15 group-hover:opacity-30 transition-opacity duration-500 ${gradient}`}
    />
    
    <div className="relative">
      <div className="mb-3 md:mb-4">
        {icon}
      </div>
      <div className="text-2xl md:text-3xl font-black text-foreground mb-1.5 tracking-tight">
        {value}
      </div>
      <div className="text-xs md:text-sm text-muted-foreground leading-relaxed">
        {description}
      </div>
    </div>
  </div>
);

const StatsCards = () => {
  const stats: StatCardProps[] = [
    {
      value: "3,000+",
      description: "Alerts per day in the average SOC — most are false positives",
      icon: (
        <div className="w-10 h-10 md:w-11 md:h-11 rounded-xl bg-gradient-to-br from-[hsl(var(--status-warning))] to-[hsl(var(--status-warning-dark))] flex items-center justify-center shadow-md shadow-[hsl(var(--status-warning))]/20">
          <AlertTriangle className="w-5 h-5 md:w-6 md:h-6 text-background" />
        </div>
      ),
      gradient: "bg-[hsl(var(--status-warning))]",
      delay: 0,
    },
    {
      value: "$4.45M",
      description: "Average cost of a data breach in 2024, up 15% from last year",
      icon: (
        <div className="w-10 h-10 md:w-11 md:h-11 rounded-xl bg-gradient-to-br from-destructive to-[hsl(var(--status-critical-dark))] flex items-center justify-center shadow-md shadow-destructive/20">
          <DollarSign className="w-5 h-5 md:w-6 md:h-6 text-foreground" />
        </div>
      ),
      gradient: "bg-destructive",
      delay: 100,
    },
    {
      value: "277",
      description: "Days on average to identify and contain a breach",
      icon: (
        <div className="w-10 h-10 md:w-11 md:h-11 rounded-xl bg-gradient-to-br from-frost to-frost-glow flex items-center justify-center shadow-md shadow-frost/20">
          <Clock className="w-5 h-5 md:w-6 md:h-6 text-background" />
        </div>
      ),
      gradient: "bg-frost",
      delay: 200,
    },
  ];

  return (
    <div className="grid grid-cols-1 md:grid-cols-3 gap-3 md:gap-4">
      {stats.map((stat) => (
        <StatCard key={stat.value} {...stat} />
      ))}
    </div>
  );
};

export default StatsCards;