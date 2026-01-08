import { Database, Server } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { DatabaseData, displayValue } from "@/types/system-health";

interface DatabasePanelProps {
  data: DatabaseData;
}

const DatabasePanel = ({ data }: DatabasePanelProps) => {
  return (
    <div className="grid grid-cols-1 sm:grid-cols-2 gap-4 mb-6">
      {/* CockroachDB */}
      <Card className="glass-frost border-border/50">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base font-medium text-foreground">
            <Database className="w-4 h-4 text-primary" />
            CockroachDB
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Database</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.cockroach.database)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Latency</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.cockroach.latency)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Pool</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.cockroach.pool)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Query P95</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.cockroach.queryP95)}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* Redis */}
      <Card className="glass-frost border-border/50">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base font-medium text-foreground">
            <Server className="w-4 h-4 text-primary" />
            Redis
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Mode</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.redis.mode)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Role</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.redis.role)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Latency</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.redis.latency)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Clients</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.redis.clients)}
            </span>
          </div>
        </CardContent>
      </Card>
    </div>
  );
};

export default DatabasePanel;
