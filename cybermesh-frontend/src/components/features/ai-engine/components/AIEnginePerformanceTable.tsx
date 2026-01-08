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
      <Badge className="bg-emerald-500/20 text-emerald-400 border-emerald-500/30 hover:bg-emerald-500/30">
        {status}
      </Badge>
    );
  }
  return (
    <Badge className="bg-amber-500/20 text-amber-400 border-amber-500/30 hover:bg-amber-500/30">
      {status}
    </Badge>
  );
};

const AIEnginePerformanceTable = ({ engines = [] }: AIEnginePerformanceTableProps) => {
  return (
    <Card className="glass-frost border-border/50 backdrop-blur-xl">
      <CardHeader className="pb-3">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-primary/10">
            <Cpu className="w-5 h-5 text-primary" />
          </div>
          <div>
            <CardTitle className="text-lg font-semibold text-foreground">AI Engine Performance</CardTitle>
            <p className="text-xs text-muted-foreground mt-0.5">Real-time</p>
          </div>
        </div>
      </CardHeader>
      <CardContent>
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="border-border/50 hover:bg-transparent">
                <TableHead className="text-muted-foreground font-medium">Engine</TableHead>
                <TableHead className="text-muted-foreground font-medium">Status</TableHead>
                <TableHead className="text-muted-foreground font-medium">Throughput</TableHead>
                <TableHead className="text-muted-foreground font-medium">Publish Rate</TableHead>
                <TableHead className="text-muted-foreground font-medium">Avg Latency</TableHead>
                <TableHead className="text-muted-foreground font-medium">Confidence</TableHead>
                <TableHead className="text-muted-foreground font-medium text-right">Published</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {engines.length === 0 ? (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={7} className="h-24 text-center text-muted-foreground">
                    No engine performance data available.
                  </TableCell>
                </TableRow>
              ) : (
                engines.map((engine) => (
                  <TableRow key={engine.name} className="border-border/30 hover:bg-muted/10">
                    <TableCell className="font-medium text-foreground">{engine.name}</TableCell>
                    <TableCell>{getStatusBadge(engine.status)}</TableCell>
                    <TableCell className="text-muted-foreground">{engine.throughput}</TableCell>
                    <TableCell className="text-muted-foreground">{engine.publishRate}</TableCell>
                    <TableCell className={parseFloat(engine.avgLatency) > 500 ? "text-amber-400" : "text-muted-foreground"}>
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
