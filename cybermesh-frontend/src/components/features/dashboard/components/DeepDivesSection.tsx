import { cn } from "@/lib/utils";
import { Activity, Brain, ArrowRight, LucideIcon } from "lucide-react";
import { Link } from "react-router-dom";
import { DeepDiveItem } from "@/mocks/dashboard";

const iconMap: Record<string, LucideIcon> = {
  Activity,
  Brain,
};

interface DeepDivesSectionProps {
  className?: string;
  deepDives: DeepDiveItem[];
}

const DeepDivesSection = ({ className, deepDives }: DeepDivesSectionProps) => {
  return (
    <div className={cn("space-y-4", className)}>
      <div>
        <h3 className="text-lg font-semibold text-foreground">Deep dives</h3>
        <p className="text-sm text-muted-foreground">
          Jump into detailed telemetry views. Use these shortcuts for rapid triage and remediation workflows.
        </p>
      </div>
      
      <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
        {deepDives.map((item) => {
          const IconComponent = iconMap[item.icon] || Activity;
          return (
            <Link
              key={item.title}
              to={item.href}
              className="group p-5 rounded-xl glass-frost text-left transition-all duration-300 hover:scale-[1.02] hover:frost-glow"
            >
              <div className="flex items-start justify-between">
                <div className="flex items-center gap-3">
                  <div className="p-2 rounded-lg bg-frost/10">
                    <IconComponent className="w-5 h-5 text-frost" />
                  </div>
                  <div>
                    <h4 className="font-semibold text-foreground">{item.title}</h4>
                    <p className="text-sm text-muted-foreground">{item.description}</p>
                  </div>
                </div>
                <ArrowRight className="w-5 h-5 text-frost opacity-0 group-hover:opacity-100 group-hover:translate-x-1 transition-all" />
              </div>
            </Link>
          );
        })}
      </div>
    </div>
  );
};

export default DeepDivesSection;
