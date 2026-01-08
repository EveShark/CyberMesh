import { useEffect, useRef, useState } from "react";
import { AlertTriangle, DollarSign, Clock, TrendingUp } from "lucide-react";

interface CountUpProps {
  end: number;
  prefix?: string;
  suffix?: string;
  duration?: number;
  isVisible: boolean;
}

const CountUp = ({ end, prefix = "", suffix = "", duration = 2000, isVisible }: CountUpProps) => {
  const [count, setCount] = useState(0);

  useEffect(() => {
    if (!isVisible) return;

    let startTime: number;
    let animationFrame: number;

    const animate = (timestamp: number) => {
      if (!startTime) startTime = timestamp;
      const progress = Math.min((timestamp - startTime) / duration, 1);
      
      // Easing function for smooth deceleration
      const easeOutQuart = 1 - Math.pow(1 - progress, 4);
      setCount(Math.floor(easeOutQuart * end));

      if (progress < 1) {
        animationFrame = requestAnimationFrame(animate);
      }
    };

    animationFrame = requestAnimationFrame(animate);

    return () => {
      if (animationFrame) {
        cancelAnimationFrame(animationFrame);
      }
    };
  }, [end, duration, isVisible]);

  return (
    <span className={isVisible ? "animate-count-glow" : ""}>
      {prefix}{count.toLocaleString()}{suffix}
    </span>
  );
};

const BentoStats = () => {
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
      { threshold: 0.2 }
    );

    if (containerRef.current) {
      observer.observe(containerRef.current);
    }

    return () => observer.disconnect();
  }, []);

  return (
    <div ref={containerRef} className="w-full max-w-4xl mx-auto">
      {/* Bento Grid */}
      <div className="grid grid-cols-2 md:grid-cols-4 gap-2 md:gap-3">
        {/* Large stat - spans 2 columns */}
        <div className="col-span-2 row-span-2 bento-card rounded-xl p-4 md:p-5 relative overflow-hidden group">
          <div className="absolute top-0 right-0 w-32 h-32 bg-ember/10 rounded-full blur-3xl group-hover:bg-ember/20 transition-all duration-500" />
          
          <div className="relative h-full flex flex-col">
            <div className="w-10 h-10 md:w-11 md:h-11 rounded-xl gradient-ember flex items-center justify-center mb-3 ember-glow-sm">
              <AlertTriangle className="w-5 h-5 md:w-6 md:h-6 text-primary-foreground" />
            </div>
            
            <div className="flex-1 flex flex-col justify-center">
              <div className="text-3xl md:text-4xl lg:text-5xl font-black text-foreground mb-2 tracking-tight">
                <CountUp end={3000} suffix="+" isVisible={isVisible} />
              </div>
              <p className="text-xs md:text-sm text-muted-foreground leading-relaxed">
                Alerts per day in the average SOC — most are false positives drowning your team
              </p>
            </div>
            
            <div className="mt-3 flex items-center gap-2">
              <div className="h-1 flex-1 bg-secondary rounded-full overflow-hidden">
                <div 
                  className={`h-full gradient-ember rounded-full ${isVisible ? 'animate-timeline-fill' : 'w-0'}`}
                  style={{ animationDelay: '0.5s' }}
                />
              </div>
              <span className="text-[10px] text-ember font-medium">OVERLOAD</span>
            </div>
          </div>
        </div>

        {/* Cost stat */}
        <div className="bento-card rounded-xl p-3 md:p-4 relative overflow-hidden group">
          <div className="absolute -top-4 -right-4 w-16 h-16 bg-crimson/10 rounded-full blur-2xl group-hover:bg-crimson/20 transition-all duration-500" />
          
          <div className="relative">
            <div className="w-8 h-8 md:w-9 md:h-9 rounded-lg bg-gradient-to-br from-crimson to-crimson-glow flex items-center justify-center mb-2 shadow-md shadow-crimson/20">
              <DollarSign className="w-4 h-4 md:w-5 md:h-5 text-foreground" />
            </div>
            
            <div className="text-xl md:text-2xl font-black text-foreground mb-1">
              $<CountUp end={4} suffix=".45M" isVisible={isVisible} duration={1500} />
            </div>
            <p className="text-[10px] md:text-xs text-muted-foreground leading-relaxed">
              Average breach cost in 2024
            </p>
          </div>
        </div>

        {/* Time stat */}
        <div className="bento-card rounded-xl p-3 md:p-4 relative overflow-hidden group">
          <div className="absolute -top-4 -right-4 w-16 h-16 bg-frost/10 rounded-full blur-2xl group-hover:bg-frost/20 transition-all duration-500" />
          
          <div className="relative">
            <div className="w-8 h-8 md:w-9 md:h-9 rounded-lg bg-gradient-to-br from-frost to-frost-glow flex items-center justify-center mb-2 shadow-md shadow-frost/20">
              <Clock className="w-4 h-4 md:w-5 md:h-5 text-primary-foreground" />
            </div>
            
            <div className="text-xl md:text-2xl font-black text-foreground mb-1">
              <CountUp end={277} isVisible={isVisible} duration={1800} />
            </div>
            <p className="text-[10px] md:text-xs text-muted-foreground leading-relaxed">
              Days to identify a breach
            </p>
          </div>
        </div>

        {/* Wide callout bar - spans 2 columns */}
        <div className="col-span-2 bento-card rounded-xl p-3 md:p-4 relative overflow-hidden border-ember/30">
          <div className="absolute inset-0 bg-gradient-to-r from-ember/5 via-transparent to-spark/5" />
          
          <div className="relative flex items-center gap-3">
            <div className="w-8 h-8 md:w-9 md:h-9 rounded-lg bg-gradient-to-br from-spark to-spark-glow flex items-center justify-center shadow-md shadow-spark/20 flex-shrink-0">
              <TrendingUp className="w-4 h-4 md:w-5 md:h-5 text-primary-foreground" />
            </div>
            
            <div className="flex-1 min-w-0">
              <div className="flex items-baseline gap-2">
                <span className="text-lg md:text-xl font-black text-spark">
                  +<CountUp end={15} suffix="%" isVisible={isVisible} duration={1200} />
                </span>
                <span className="text-xs text-muted-foreground">YoY increase</span>
              </div>
              <p className="text-[10px] md:text-xs text-muted-foreground truncate">
                Breach costs rising faster than security budgets
              </p>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default BentoStats;
