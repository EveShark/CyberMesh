import { ReactNode } from "react";

interface ValuePillarProps {
  icon: ReactNode;
  title: string;
  description: string;
}

const ValuePillar = ({ icon, title, description }: ValuePillarProps) => {
  return (
    <div className="group p-6 rounded-lg glass-frost transition-all duration-300 hover:border-frost/30 hover:scale-[1.02]">
      <div className="w-12 h-12 rounded-lg bg-frost/10 flex items-center justify-center mb-4 text-frost transition-all duration-300 group-hover:bg-frost/20 group-hover:frost-glow">
        {icon}
      </div>
      <h3 className="text-lg font-semibold text-foreground mb-2">{title}</h3>
      <p className="text-muted-foreground text-sm leading-relaxed">{description}</p>
    </div>
  );
};

export default ValuePillar;