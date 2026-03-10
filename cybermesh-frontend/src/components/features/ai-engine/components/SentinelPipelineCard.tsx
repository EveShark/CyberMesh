import { Activity, Radar } from "lucide-react";

import type { SentinelPerformance } from "@/types/ai-engine";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

interface SentinelPipelineCardProps {
  data: SentinelPerformance | null;
}

function statusBadge(status: SentinelPerformance["status"]) {
  if (status === "Active") {
    return <Badge className="bg-status-healthy/10 text-status-healthy border-status-healthy/30 hover:bg-status-healthy/15">{status}</Badge>;
  }
  if (status === "Observing") {
    return <Badge className="bg-status-warning/10 text-status-warning border-status-warning/30 hover:bg-status-warning/15">{status}</Badge>;
  }
  return <Badge variant="outline">{status}</Badge>;
}

const SentinelPipelineCard = ({ data }: SentinelPipelineCardProps) => {
  return (
    <Card className="glass-frost border-border/60">
      <CardHeader className="pb-4">
        <div className="flex items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-primary/10">
              <Radar className="w-5 h-5 text-primary" />
            </div>
            <div>
              <CardTitle className="text-lg font-semibold tracking-tight text-foreground">Sentinel Pipeline</CardTitle>
              <p className="text-xs leading-relaxed text-muted-foreground mt-0.5">Transport and publish health</p>
            </div>
          </div>
          {data ? statusBadge(data.status) : <Badge variant="outline">Idle</Badge>}
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        {!data ? (
          <div className="h-24 flex items-center justify-center text-sm text-muted-foreground">
            No Sentinel pipeline activity recorded yet.
          </div>
        ) : (
          <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
            <Metric label="Events" value={data.eventsTotal.toLocaleString()} />
            <Metric label="Published Detections" value={data.detectionsTotal.toLocaleString()} />
            <Metric label="Publish Rate" value={data.publishRate} />
            <Metric label="Top Threat" value={data.topThreat ?? "--"} />
            <Metric label={data.entityLabel ? `Last ${data.entityLabel}` : "Last Entity"} value={data.lastEntity ?? "--"} />
            <Metric label="Last Detection" value={data.lastDetection ?? "--"} />
          </div>
        )}
      </CardContent>
    </Card>
  );
};

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="rounded-xl border border-border/50 bg-background/60 px-4 py-3">
      <div className="flex items-center gap-2 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">
        <Activity className="w-3.5 h-3.5" />
        <span>{label}</span>
      </div>
      <div className="mt-2 text-base font-semibold text-foreground break-all">{value}</div>
    </div>
  );
}

export default SentinelPipelineCard;
