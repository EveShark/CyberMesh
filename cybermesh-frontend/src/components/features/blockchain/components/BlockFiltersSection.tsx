import { Search } from "lucide-react";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";

interface BlockFiltersSectionProps {
  searchQuery: string;
  onSearchChange: (value: string) => void;
  blockType: string;
  onBlockTypeChange: (value: string) => void;
  timeRange: string;
  onTimeRangeChange: (value: string) => void;
  proposer: string;
  onProposerChange: (value: string) => void;
  proposers: string[];
}

const BlockFiltersSection = ({
  searchQuery,
  onSearchChange,
  blockType,
  onBlockTypeChange,
  timeRange,
  onTimeRangeChange,
  proposer,
  onProposerChange,
  proposers,
}: BlockFiltersSectionProps) => {
  return (
    <div className="glass-frost rounded-lg p-4">
      <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
        {/* Search by Height/Hash */}
        <div className="relative">
          <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
          <Input
            placeholder="Search by Height/Hash..."
            value={searchQuery}
            onChange={(e) => onSearchChange(e.target.value)}
            className="pl-9 bg-background/50 border-border"
          />
        </div>

        {/* Block Type */}
        <Select value={blockType} onValueChange={onBlockTypeChange}>
          <SelectTrigger className="bg-background/50 border-border">
            <SelectValue placeholder="Block Type" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Types</SelectItem>
            <SelectItem value="normal">Normal</SelectItem>
            <SelectItem value="anomaly">Anomaly</SelectItem>
          </SelectContent>
        </Select>

        {/* Time Range */}
        <Select value={timeRange} onValueChange={onTimeRangeChange}>
          <SelectTrigger className="bg-background/50 border-border">
            <SelectValue placeholder="Time Range" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Time</SelectItem>
            <SelectItem value="1h">Last 1 hour</SelectItem>
            <SelectItem value="24h">Last 24 hours</SelectItem>
            <SelectItem value="7d">Last 7 days</SelectItem>
            <SelectItem value="30d">Last 30 days</SelectItem>
          </SelectContent>
        </Select>

        {/* Proposer */}
        <Select value={proposer} onValueChange={onProposerChange}>
          <SelectTrigger className="bg-background/50 border-border">
            <SelectValue placeholder="Proposer" />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="all">All Proposers</SelectItem>
            {proposers.map((p) => (
              <SelectItem key={p} value={p}>
                {p.slice(0, 10)}...{p.slice(-6)}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      </div>
    </div>
  );
};

export default BlockFiltersSection;
