import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Radio, ChevronDown, Inbox, ExternalLink } from "lucide-react";
import { useState } from "react";
import { DetectionStreamData, DetectionEvent } from "@/types/ai-engine";
import { DetectionDetailModal } from "./DetectionDetailModal";

interface DetectionStreamFeedProps {
  data: DetectionStreamData;
}

const DetectionStreamFeed = ({ data }: DetectionStreamFeedProps) => {
  const [showMore, setShowMore] = useState(false);
  const [selectedEvent, setSelectedEvent] = useState<DetectionEvent | null>(null);

  const formatTime = (dateString: string) => {
    const date = new Date(dateString);
    return date.toLocaleString("en-US", {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      second: "2-digit",
      hour12: true,
    });
  };

  const hasEvents = data.events.length > 0;
  const displayedEvents = showMore ? data.events : data.events.slice(0, 3);
  const hiddenCount = data.events.length > 3 ? data.events.length - 3 : 0;
  const publishRate = data.totalCount > 0 ? ((data.publishedCount / data.totalCount) * 100).toFixed(1) : "0.0";

  return (
    <>
      <Card className="glass-frost border-border/50 backdrop-blur-xl">
        <CardHeader className="pb-3">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-primary/10">
              <Radio className="w-5 h-5 text-primary" />
            </div>
            <div className="flex-1">
              <CardTitle className="text-lg font-semibold text-foreground">Real-Time Detection Stream</CardTitle>
            </div>
            {hasEvents && (
              <div className="flex items-center gap-2">
                <span className="relative flex h-2 w-2">
                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-emerald-400 opacity-75"></span>
                  <span className="relative inline-flex rounded-full h-2 w-2 bg-emerald-500"></span>
                </span>
                <span className="text-xs text-emerald-400">Live</span>
              </div>
            )}
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {hasEvents ? (
            <>
              <div className="space-y-3 max-h-96 overflow-y-auto pr-2">
                {displayedEvents.map((event) => (
                  <div
                    key={event.id}
                    className="p-4 rounded-lg bg-muted/10 border border-border/30 space-y-2 hover:bg-muted/20 transition-all cursor-pointer group relative"
                    onClick={() => setSelectedEvent(event)}
                  >
                    <div className="flex items-center justify-between">
                      <Badge className="bg-destructive/20 text-destructive border-destructive/30 uppercase text-xs font-bold">
                        {event.threatType}
                      </Badge>
                      <div className="flex items-center gap-2">
                        <span className="text-xs text-muted-foreground">{formatTime(event.timestamp)}</span>
                        <ExternalLink className="w-3 h-3 text-muted-foreground opacity-0 group-hover:opacity-100 transition-opacity" />
                      </div>
                    </div>

                    <div className="flex items-center gap-4">
                      <span className="text-sm text-foreground">
                        <span className="text-emerald-400 font-semibold">{event.confidence}%</span> confidence
                      </span>
                    </div>

                    <div className="flex items-center gap-4 text-xs text-muted-foreground">
                      <span>
                        Final score: <span className="text-foreground font-medium">{event.finalScore}%</span>
                      </span>
                      <span>
                        Decision: <span className="text-emerald-400 font-medium">{event.decision}</span>
                      </span>
                    </div>

                    {event.metadata && Object.keys(event.metadata).length > 0 && (
                      <div className="pt-2 border-t border-border/30 flex flex-wrap gap-3">
                        {event.metadata.candidate_count != null && (
                          <span className="text-xs text-muted-foreground">
                            <span className="text-foreground/70">Candidates:</span>{" "}
                            <span className="font-medium text-foreground">{event.metadata.candidate_count}</span>
                          </span>
                        )}
                        {event.metadata.latency_ms != null && (
                          <span className="text-xs text-muted-foreground">
                            <span className="text-foreground/70">Latency:</span>{" "}
                            <span className="font-medium text-foreground">
                              {typeof event.metadata.latency_ms === 'number'
                                ? event.metadata.latency_ms.toFixed(1) + 'ms'
                                : event.metadata.latency_ms}
                            </span>
                          </span>
                        )}
                      </div>
                    )}
                  </div>
                ))}
              </div>

              {data.events.length > 3 && (
                <div className="flex items-center justify-center pt-2">
                  <Button
                    variant="ghost"
                    onClick={() => setShowMore(!showMore)}
                    className="text-muted-foreground hover:text-foreground"
                  >
                    <ChevronDown className={`w-4 h-4 mr-2 transition-transform ${showMore ? "rotate-180" : ""}`} />
                    {showMore ? "Show Less" : `Show More (${hiddenCount} hidden)`}
                  </Button>
                </div>
              )}
            </>
          ) : (
            <div className="p-12 rounded-lg bg-muted/5 border border-border/30 text-center">
              <Inbox className="w-12 h-12 text-muted-foreground mx-auto mb-3" />
              <p className="text-foreground font-medium">No detection history</p>
              <p className="text-xs text-muted-foreground mt-1">Detection events will appear here when the loop is active</p>
            </div>
          )}

          <div className="pt-4 border-t border-border/50">
            <div className="flex items-center justify-center gap-2 text-sm">
              <span className="text-muted-foreground">Summary:</span>
              <span className="text-foreground font-medium">{data.totalCount} total</span>
              <span className="text-muted-foreground">|</span>
              <span className="text-emerald-400 font-medium">
                {data.publishedCount} published ({publishRate}%)
              </span>
              <span className="text-muted-foreground">|</span>
              <span className="text-amber-400 font-medium">{data.heldForReview} held for review</span>
            </div>
          </div>
        </CardContent>
      </Card>

      <DetectionDetailModal
        event={selectedEvent}
        isOpen={!!selectedEvent}
        onClose={() => setSelectedEvent(null)}
      />
    </>
  );
};

export default DetectionStreamFeed;
