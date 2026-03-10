import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Cpu } from "lucide-react";

export interface EnginePerformance {
  name: string;
  status: string;
  throughput: string;
  publishRate: string;
  avgLatency: string;
  confidence: string;
  published: number;
}

interface AIEnginePerformanceTableProps {
  engines?: EnginePerformance[];
}

const getStatusBadge = (status: string) => {
  if (status === "Healthy") {
    return (
      <Badge className="bg-status-healthy/10 text-status-healthy border-status-healthy/30 hover:bg-status-healthy/15">
        {status}
      </Badge>
    );
  }
  return (
    <Badge className="bg-status-warning/10 text-status-warning border-status-warning/30 hover:bg-status-warning/15">
      {status}
    </Badge>
  );
};

const AIEnginePerformanceTable = ({ engines = [] }: AIEnginePerformanceTableProps) => {
  return (
    <Card className="glass-frost border-border/60">
      <CardHeader className="pb-4">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-primary/10">
            <Cpu className="w-5 h-5 text-primary" />
          </div>
          <div>
            <CardTitle className="text-lg font-semibold tracking-tight text-foreground">Detection Engines</CardTitle>
            <p className="text-xs leading-relaxed text-muted-foreground mt-0.5">Rules, math, and ML inference activity</p>
          </div>
        </div>
      </CardHeader>
      <CardContent className="pt-0">
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="border-border/50 hover:bg-transparent">
                <TableHead className="text-[11px] tracking-wide text-muted-foreground font-medium">Engine</TableHead>
                <TableHead className="text-[11px] tracking-wide text-muted-foreground font-medium">Status</TableHead>
                <TableHead className="text-[11px] tracking-wide text-muted-foreground font-medium">Throughput</TableHead>
                <TableHead className="text-[11px] tracking-wide text-muted-foreground font-medium">Publish Rate</TableHead>
                <TableHead className="text-[11px] tracking-wide text-muted-foreground font-medium">Avg Latency</TableHead>
                <TableHead className="text-[11px] tracking-wide text-muted-foreground font-medium">Confidence</TableHead>
                <TableHead className="text-[11px] tracking-wide text-muted-foreground font-medium text-right">Published</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {engines.length === 0 ? (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={7} className="h-24 text-center text-muted-foreground">
                    No per-engine metrics recorded yet.
                  </TableCell>
                </TableRow>
              ) : (
                engines.map((engine) => (
                  <TableRow key={engine.name} className="border-border/30 hover:bg-muted/10">
                    <TableCell className="font-medium text-foreground">{engine.name}</TableCell>
                    <TableCell>{getStatusBadge(engine.status)}</TableCell>
                    <TableCell className="text-muted-foreground">{engine.throughput}</TableCell>
                    <TableCell className="text-muted-foreground">{engine.publishRate}</TableCell>
                    <TableCell className={parseFloat(engine.avgLatency) > 500 ? "text-status-warning" : "text-muted-foreground"}>
                      {engine.avgLatency}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{engine.confidence}</TableCell>
                    <TableCell className="text-right font-semibold text-foreground">
                      {engine.published.toLocaleString()}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  );
};

export default AIEnginePerformanceTable;
