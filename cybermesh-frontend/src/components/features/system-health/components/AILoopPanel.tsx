import { Activity, Zap, Clock, TrendingUp } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { AILoopData, getStatusColors, displayValue } from "@/types/system-health";

interface AILoopPanelProps {
  data: AILoopData;
}

const AILoopPanel = ({ data }: AILoopPanelProps) => {
  const statusColors = getStatusColors(data.loopStatus);

  return (
    <Card className="glass-frost border-border/50">
      <CardHeader className="pb-4">
        <CardTitle className="flex items-center gap-2 text-lg font-semibold text-foreground">
          <Activity className="w-5 h-5 text-primary" />
          AI Detection Loop
        </CardTitle>
      </CardHeader>
      <CardContent>
        <div className="flex flex-col lg:flex-row lg:items-center gap-6">
          {/* Large Status Badge */}
          <div className="flex items-center gap-4">
            <Badge className={`${statusColors} px-6 py-3 text-lg font-bold border`}>
              {data.loopStatus}
            </Badge>
          </div>

          {/* Metrics Grid */}
          <div className="grid grid-cols-1 sm:grid-cols-3 gap-6 flex-1">
            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-primary/10 border border-primary/20">
                <Zap className="w-4 h-4 text-primary" />
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Avg Latency</p>
                <p className="text-sm font-semibold text-foreground">
                  {displayValue(data.avgLatency)}
                </p>
              </div>
            </div>

            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-primary/10 border border-primary/20">
                <Clock className="w-4 h-4 text-primary" />
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Last Iteration</p>
                <p className="text-sm font-semibold text-foreground">
                  {displayValue(data.lastIteration)}
                </p>
              </div>
            </div>

            <div className="flex items-center gap-3">
              <div className="p-2 rounded-lg bg-primary/10 border border-primary/20">
                <TrendingUp className="w-4 h-4 text-primary" />
              </div>
              <div>
                <p className="text-xs text-muted-foreground">Publish Rate</p>
                <p className="text-sm font-semibold text-foreground">
                  {displayValue(data.publishRate)}
                </p>
              </div>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
};

export default AILoopPanel;
