import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { ServiceInfo, getStatusColors, displayValue } from "@/types/system-health";
import { Server } from "lucide-react";

interface ServiceHealthGridProps {
  services: ServiceInfo[];
}

const getServiceMetrics = (service: ServiceInfo): string => {
  const metrics: string[] = [];

  if (service.latency) metrics.push(`Latency: ${service.latency}`);
  if (service.queryP95) metrics.push(`Query P95: ${service.queryP95}`);
  if (service.pool) metrics.push(`Pool: ${service.pool}`);
  if (service.version !== undefined) metrics.push(`Version: ${service.version}`);
  if (service.success) metrics.push(`Success: ${service.success}`);
  if (service.pending !== undefined) metrics.push(`Pending: ${service.pending}`);
  if (service.size) metrics.push(`Size: ${service.size}`);
  if (service.oldest) metrics.push(`Oldest: ${service.oldest}`);
  if (service.view !== undefined) metrics.push(`View: ${service.view}`);
  if (service.round !== undefined) metrics.push(`Round: ${service.round}`);
  if (service.quorum !== undefined) metrics.push(`Quorum: ${service.quorum}`);
  if (service.peers) metrics.push(`Peers: ${service.peers}`);
  if (service.throughput) metrics.push(`Throughput: ${service.throughput}`);
  if (service.publishP95) metrics.push(`Publish P95: ${service.publishP95}`);
  if (service.lag !== undefined) metrics.push(`Lag: ${service.lag}`);
  if (service.highwater) metrics.push(`Highwater: ${service.highwater}`);
  if (service.clients !== undefined) metrics.push(`Clients: ${displayValue(service.clients)}`);
  if (service.errors !== undefined) metrics.push(`Errors: ${service.errors}`);
  if (service.txnP95) metrics.push(`Txn P95: ${service.txnP95}`);
  if (service.slowTxns !== undefined) metrics.push(`Slow Txns: ${service.slowTxns}`);
  if (service.loop) metrics.push(`Loop: ${service.loop}`);
  if (service.publishMin !== undefined) metrics.push(`Publish/min: ${service.publishMin}`);
  if (service.detectionsMin !== undefined) metrics.push(`Detections/min: ${service.detectionsMin}`);

  return metrics.join(" • ");
};

const ServiceHealthGrid = ({ services }: ServiceHealthGridProps) => {
  return (
    <Card className="glass-frost border-border/50 mb-6">
      <CardHeader className="pb-4">
        <CardTitle className="flex items-center gap-2 text-lg font-semibold text-foreground">
          <Server className="w-5 h-5 text-primary" />
          Service Health Grid
        </CardTitle>
        <p className="text-sm text-muted-foreground mt-1">
          Overall subsystem status reported by the backend
        </p>
      </CardHeader>
      <CardContent>
        <div className="rounded-lg border border-border/50 overflow-hidden">
          <Table>
            <TableHeader>
              <TableRow className="bg-muted/10 hover:bg-muted/10">
                <TableHead className="text-muted-foreground font-medium">Component</TableHead>
                <TableHead className="text-muted-foreground font-medium">Status</TableHead>
                <TableHead className="text-muted-foreground font-medium">Metrics</TableHead>
                <TableHead className="text-muted-foreground font-medium">Issues</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {services.map((service, index) => {
                const statusColors = getStatusColors(service.status);
                const hasIssue = service.issues !== "No issues reported";
                
                return (
                  <TableRow 
                    key={service.name} 
                    className={`${index % 2 === 0 ? "bg-transparent" : "bg-muted/5"} hover:bg-muted/10 transition-colors`}
                  >
                    <TableCell className="font-medium text-foreground">
                      {service.name}
                    </TableCell>
                    <TableCell>
                      <Badge className={`${statusColors} text-xs border capitalize`}>
                        {service.status}
                      </Badge>
                    </TableCell>
                    <TableCell className="text-sm text-muted-foreground max-w-md">
                      <span className="line-clamp-2">
                        {getServiceMetrics(service)}
                      </span>
                    </TableCell>
                    <TableCell className={`text-sm ${hasIssue ? "text-amber-400" : "text-muted-foreground"}`}>
                      {service.issues}
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        </div>
      </CardContent>
    </Card>
  );
};

export default ServiceHealthGrid;
