import { ReactNode } from "react";
import { useScrollReveal } from "@/hooks/ui/use-scroll-reveal";
import { cn } from "@/lib/utils";

interface ScrollRevealSectionProps {
  children: ReactNode;
  className?: string;
  delay?: number;
  direction?: "up" | "left" | "right" | "scale";
}

const ScrollRevealSection = ({
  children,
  className,
  delay = 0,
  direction = "up",
}: ScrollRevealSectionProps) => {
  const [ref, isVisible] = useScrollReveal<HTMLDivElement>({ threshold: 0.1 });

  const getTransform = () => {
    if (isVisible) return "translate3d(0, 0, 0) scale(1)";
    switch (direction) {
      case "up":
        return "translate3d(0, 60px, 0)";
      case "left":
        return "translate3d(-60px, 0, 0)";
      case "right":
        return "translate3d(60px, 0, 0)";
      case "scale":
        return "translate3d(0, 30px, 0) scale(0.95)";
      default:
        return "translate3d(0, 60px, 0)";
    }
  };

  return (
    <div
      ref={ref}
      className={cn("transition-all duration-700 ease-out", className)}
      style={{
        opacity: isVisible ? 1 : 0,
        transform: getTransform(),
        transitionDelay: `${delay}ms`,
      }}
    >
      {children}
    </div>
  );
};

export default ScrollRevealSection;
