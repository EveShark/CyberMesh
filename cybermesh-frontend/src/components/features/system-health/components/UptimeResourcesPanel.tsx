import { Clock, Cpu, HardDrive, Activity } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { SystemHealthData, displayValue, getStatusColors } from "@/types/system-health";

interface UptimeResourcesPanelProps {
  data: SystemHealthData;
}

const UptimeResourcesPanel = ({ data }: UptimeResourcesPanelProps) => {
  const aiStateColors = getStatusColors(data.aiUptime.state === "running" ? "healthy" : "halted");

  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 mb-6">
      {/* Backend Uptime */}
      <Card className="glass-frost border-border/50">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base font-medium text-foreground">
            <Clock className="w-4 h-4 text-primary" />
            Backend Uptime
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Started</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.backendUptime.started)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Runtime</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.backendUptime.runtime)}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* AI Uptime */}
      <Card className="glass-frost border-border/50">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base font-medium text-foreground">
            <Activity className="w-4 h-4 text-primary" />
            AI Uptime
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">State</span>
            <Badge className={`${aiStateColors} text-xs border`}>
              {data.aiUptime.state}
            </Badge>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Loop Status</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.aiUptime.loopStatus)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Detection Loop</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.aiUptime.detectionLoopUptime)}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* System Resources */}
      <Card className="glass-frost border-border/50">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base font-medium text-foreground">
            <Cpu className="w-4 h-4 text-primary" />
            System Resources
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">CPU (avg)</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.resources.cpu.avgUtilization)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Memory</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.resources.memory.resident)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Allocated</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.resources.memory.allocated)}
            </span>
          </div>
        </CardContent>
      </Card>
    </div>
  );
};

export default UptimeResourcesPanel;
