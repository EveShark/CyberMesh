import { ChevronRight } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import type { ThreatBreakdown, SystemStatus, ThreatSeverity } from "@/types/threats";

interface DetectionBreakdownTableProps {
  breakdown: ThreatBreakdown[];
  systemStatus: SystemStatus;
}

const getSeverityColor = (severity: ThreatSeverity) => {
  switch (severity) {
    case "CRITICAL":
      return "bg-destructive/20 text-destructive border-destructive/30";
    case "HIGH":
      return "bg-orange-500/20 text-orange-400 border-orange-500/30";
    case "MEDIUM":
      return "bg-amber-500/20 text-amber-400 border-amber-500/30";
    case "LOW":
      return "bg-emerald-500/20 text-emerald-400 border-emerald-500/30";
    default:
      return "bg-muted/20 text-muted-foreground border-muted/30";
  }
};

const DetectionBreakdownTable = ({ breakdown, systemStatus }: DetectionBreakdownTableProps) => {
  const isLive = systemStatus === "LIVE";
  const hasData = isLive && breakdown.length > 0;

  return (
    <Card className="glass-frost border-border/50">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          Aggregated AI detections by threat type
        </CardTitle>
      </CardHeader>
      <CardContent>
        {hasData ? (
          <>
            {/* Desktop Table */}
            <div className="hidden sm:block overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="border-border/50 hover:bg-transparent">
                    <TableHead className="text-muted-foreground">Threat Type</TableHead>
                    <TableHead className="text-muted-foreground">Severity</TableHead>
                    <TableHead className="text-muted-foreground text-right">Blocked</TableHead>
                    <TableHead className="text-muted-foreground text-right">Monitored</TableHead>
                    <TableHead className="text-muted-foreground text-right">Block Rate</TableHead>
                    <TableHead className="text-muted-foreground text-right">Total</TableHead>
                    <TableHead className="text-muted-foreground w-10"></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {breakdown.map((item, index) => (
                    <TableRow key={index} className="border-border/30 hover:bg-muted/10">
                      <TableCell className="font-mono font-medium text-foreground uppercase">
                        {item.type}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className={getSeverityColor(item.severity)}>
                          {item.severity}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-right font-mono text-destructive">
                        {item.blocked.toLocaleString()}
                      </TableCell>
                      <TableCell className="text-right font-mono text-muted-foreground">
                        {item.monitored.toLocaleString()}
                      </TableCell>
                      <TableCell className="text-right font-mono text-emerald-400">
                        {item.blockRate}
                      </TableCell>
                      <TableCell className="text-right font-mono font-bold text-foreground">
                        {item.total.toLocaleString()}
                      </TableCell>
                      <TableCell>
                        <ChevronRight className="h-4 w-4 text-muted-foreground" />
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>

            {/* Mobile Cards */}
            <div className="sm:hidden space-y-3">
              {breakdown.map((item, index) => (
                <div
                  key={index}
                  className="p-3 rounded-lg bg-muted/10 border border-border/30 space-y-2"
                >
                  <div className="flex items-center justify-between">
                    <span className="font-mono font-medium text-foreground uppercase">
                      {item.type}
                    </span>
                    <Badge variant="outline" className={getSeverityColor(item.severity)}>
                      {item.severity}
                    </Badge>
                  </div>
                  <div className="grid grid-cols-2 gap-2 text-sm">
                    <div>
                      <span className="text-muted-foreground">Blocked: </span>
                      <span className="font-mono text-destructive">
                        {item.blocked.toLocaleString()}
                      </span>
                    </div>
                    <div>
                      <span className="text-muted-foreground">Monitored: </span>
                      <span className="font-mono">{item.monitored.toLocaleString()}</span>
                    </div>
                    <div>
                      <span className="text-muted-foreground">Block Rate: </span>
                      <span className="font-mono text-emerald-400">{item.blockRate}</span>
                    </div>
                    <div>
                      <span className="text-muted-foreground">Total: </span>
                      <span className="font-mono font-bold">{item.total.toLocaleString()}</span>
                    </div>
                  </div>
                </div>
              ))}
            </div>
          </>
        ) : (
          <div className="py-12 text-center">
            <p className="text-muted-foreground">
              {isLive ? "No threats detected" : "System halted - no data available"}
            </p>
          </div>
        )}
      </CardContent>
    </Card>
  );
};

export default DetectionBreakdownTable;
