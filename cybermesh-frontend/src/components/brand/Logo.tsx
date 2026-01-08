import { cn } from "@/lib/utils";
import { Hexagon, Shield, Network } from "lucide-react";

interface LogoProps {
  size?: number;
  className?: string;
  variant?: "full" | "icon";
  showText?: boolean;
}

const Logo = ({
  size = 32,
  className,
  variant = "full",
  showText = true,
}: LogoProps) => {
  return (
    <div className={cn("flex items-center gap-2", className)}>
      {/* Combined icon: Hexagon with Shield inside */}
      <div className="relative" style={{ width: size, height: size }}>
        <Hexagon 
          size={size} 
          className="text-frost absolute inset-0"
          strokeWidth={1.5}
        />
        <div className="absolute inset-0 flex items-center justify-center">
          <Network 
            size={size * 0.5} 
            className="text-ember"
            strokeWidth={2}
          />
        </div>
      </div>

      {/* Text */}
      {variant === "full" && showText && (
        <span
          className="font-bold tracking-tight text-foreground"
          style={{ fontSize: size * 0.5 }}
        >
          Cyber<span className="text-frost">Mesh</span>
        </span>
      )}
    </div>
  );
};

export default Logo;
