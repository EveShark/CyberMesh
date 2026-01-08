import { Radio, Network, Shield } from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { PipelineData, displayValue } from "@/types/system-health";

interface PipelineNetworkPanelProps {
  data: PipelineData;
}

const PipelineNetworkPanel = ({ data }: PipelineNetworkPanelProps) => {
  return (
    <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4 mb-6">
      {/* Kafka Producer */}
      <Card className="glass-frost border-border/50">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base font-medium text-foreground">
            <Radio className="w-4 h-4 text-primary" />
            Kafka Producer
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Publish P95</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.kafkaProducer.publishP95)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Publish P50</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.kafkaProducer.publishP50)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Successes</span>
            <span className="text-sm font-medium text-emerald-400">
              {displayValue(data.kafkaProducer.successes)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Failures</span>
            <span className={`text-sm font-medium ${data.kafkaProducer.failures > 0 ? "text-red-400" : "text-foreground"}`}>
              {displayValue(data.kafkaProducer.failures)}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* Network & Mempool */}
      <Card className="glass-frost border-border/50">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base font-medium text-foreground">
            <Network className="w-4 h-4 text-primary" />
            Network & Mempool
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Peers</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.network.peers)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Latency</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.network.latency)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Throughput In</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.network.throughputIn)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Mempool Size</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.network.mempoolSize)}
            </span>
          </div>
        </CardContent>
      </Card>

      {/* Threat Feed */}
      <Card className="glass-frost border-border/50">
        <CardHeader className="pb-3">
          <CardTitle className="flex items-center gap-2 text-base font-medium text-foreground">
            <Shield className="w-4 h-4 text-primary" />
            Threat Feed
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Data Source</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.threatFeed.source)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">History</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.threatFeed.history)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Fallback Count</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.threatFeed.fallbackCount)}
            </span>
          </div>
          <div className="flex justify-between items-center">
            <span className="text-sm text-muted-foreground">Last Fallback</span>
            <span className="text-sm font-medium text-foreground">
              {displayValue(data.threatFeed.lastFallback)}
            </span>
          </div>
        </CardContent>
      </Card>
    </div>
  );
};

export default PipelineNetworkPanel;
