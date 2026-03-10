import { useEffect, useRef, useState } from "react";
import { AlertTriangle, X } from "lucide-react";

const threats = [
    {
        text: "GTG-1002: AI agents automated 90% of nation-state attack operations",
        source: "Anthropic, Sep 2025",
    },
    {
        text: "91,403 LLM scanning sessions targeting AI endpoints detected",
        source: "GreyNoise, Q4 2025",
    },
    {
        text: "~2,000 MCP servers exposed with zero authorization controls",
        source: "Dark Reading, Jan 2026",
    },
    {
        text: "Shai-Hulud 2.0 worm compromised hundreds of npm packages",
        source: "CISA, Sep 2025",
    },
    {
        text: "277 days average time to identify a breach",
        source: "IBM, 2025",
    },
];

const ThreatTicker = () => {
    const [isDismissed, setIsDismissed] = useState(false);
    const [isPaused, setIsPaused] = useState(false);
    const tickerRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        const dismissed = sessionStorage.getItem("threat-ticker-dismissed");
        if (dismissed === "true") {
            setIsDismissed(true);
        }
    }, []);

    const handleDismiss = () => {
        setIsDismissed(true);
        sessionStorage.setItem("threat-ticker-dismissed", "true");
    };

    if (isDismissed) return null;

    return (
        <div className="relative bento-card rounded-xl p-4 md:p-5 overflow-hidden border-ember/20">
            {/* Gradient overlay */}
            <div className="absolute inset-0 bg-gradient-to-br from-ember/5 via-transparent to-spark/5" />

            {/* Dismiss button */}
            <button
                onClick={handleDismiss}
                className="absolute top-2 right-2 z-10 p-1.5 rounded-full bg-muted/30 text-muted-foreground/60 hover:text-foreground hover:bg-muted/50 transition-all"
                aria-label="Dismiss"
            >
                <X className="w-3.5 h-3.5" />
            </button>

            <div className="relative">
                {/* Centered header */}
                <div className="text-center mb-4">
                    <div className="inline-flex items-center gap-2 px-3 py-1.5 rounded-full glass-ember border border-ember/30">
                        <span className="relative flex h-2 w-2">
                            <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-ember opacity-75" />
                            <span className="relative inline-flex rounded-full h-2 w-2 bg-ember" />
                        </span>
                        <span className="text-[10px] md:text-xs font-semibold uppercase tracking-wider text-ember">
                            Live Threat Intelligence
                        </span>
                    </div>
                </div>

                {/* Scrolling content - full width */}
                <div
                    className="relative overflow-hidden rounded-lg bg-background/40 backdrop-blur-sm border border-border/30"
                    onMouseEnter={() => setIsPaused(true)}
                    onMouseLeave={() => setIsPaused(false)}
                >
                    <div
                        ref={tickerRef}
                        className={`flex items-center gap-10 md:gap-14 py-3 px-4 whitespace-nowrap ${isPaused ? '' : 'animate-ticker'}`}
                        style={{ animationPlayState: isPaused ? 'paused' : 'running' }}
                    >
                        {/* Duplicate content for seamless loop */}
                        {[...threats, ...threats].map((threat, index) => (
                            <div
                                key={index}
                                className="flex items-center gap-2.5 group cursor-default"
                            >
                                <div className="flex-shrink-0 w-6 h-6 rounded-md bg-ember/10 flex items-center justify-center">
                                    <AlertTriangle className="w-3.5 h-3.5 text-ember" />
                                </div>
                                <div className="flex flex-col">
                                    <span className="text-xs md:text-sm text-foreground/90 group-hover:text-foreground transition-colors font-medium">
                                        {threat.text}
                                    </span>
                                    <span className="text-[9px] md:text-[10px] text-muted-foreground/60">
                                        {threat.source}
                                    </span>
                                </div>
                            </div>
                        ))}
                    </div>

                    {/* Fade edges */}
                    <div className="absolute inset-y-0 left-0 w-12 bg-gradient-to-r from-background/40 to-transparent pointer-events-none" />
                    <div className="absolute inset-y-0 right-0 w-12 bg-gradient-to-l from-background/40 to-transparent pointer-events-none" />
                </div>

                {/* Hover hint */}
                <p className="text-center mt-3 text-[9px] text-muted-foreground/50">
                    Hover to pause
                </p>
            </div>
        </div>
    );
};

export default ThreatTicker;
