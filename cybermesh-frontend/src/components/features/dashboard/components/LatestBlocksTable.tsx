import { forwardRef } from "react";
import { cn } from "@/lib/utils";
import { Blocks, Copy, ArrowRight } from "lucide-react";
import { Link } from "react-router-dom";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { toast } from "@/hooks/common/use-toast";

export interface Block {
  height: number;
  time: string;
  txs: number;
  hash: string;
  proposer: string;
}

export interface LatestBlocksData {
  blocks: Block[];
  rangeStart: number;
  rangeEnd: number;
  total: number;
}

interface LatestBlocksTableProps {
  className?: string;
  data?: LatestBlocksData;
}

const truncateHash = (hash: string, length = 8) => {
  if (hash.length <= length * 2) return hash;
  return `${hash.slice(0, length)}...${hash.slice(-length)}`;
};

const LatestBlocksTable = forwardRef<HTMLDivElement, LatestBlocksTableProps>(
  ({ className, data }, ref) => {
  const blocks = data?.blocks || [];
  const rangeStart = data?.rangeStart || 0;
  const rangeEnd = data?.rangeEnd || 0;
  const total = data?.total || 0;

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text);
    toast({
      title: "Copied to clipboard",
      description: "Hash has been copied to your clipboard.",
    });
  };

    return (
      <div ref={ref} className={cn("rounded-xl p-5 glass-frost", className)}>
        <div className="flex items-center justify-between mb-4">
          <div className="flex items-center gap-3">
            <div className="p-2 rounded-lg bg-frost/10">
              <Blocks className="w-5 h-5 text-frost" />
            </div>
            <div>
              <h3 className="font-semibold text-foreground">Latest Blocks</h3>
              <p className="text-xs text-muted-foreground">
                Showing #{rangeStart.toLocaleString()} - #{rangeEnd.toLocaleString()} of ~{total.toLocaleString()}
              </p>
            </div>
          </div>
          <Link 
            to="/blockchain"
            className="flex items-center gap-2 text-sm text-frost hover:text-frost-glow transition-colors group"
          >
            View All Blocks
            <ArrowRight className="w-4 h-4 group-hover:translate-x-1 transition-transform" />
          </Link>
        </div>

        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="border-border/50 hover:bg-transparent">
                <TableHead className="text-muted-foreground font-medium">HEIGHT</TableHead>
                <TableHead className="text-muted-foreground font-medium">TIME</TableHead>
                <TableHead className="text-muted-foreground font-medium">TXS</TableHead>
                <TableHead className="text-muted-foreground font-medium">HASH</TableHead>
                <TableHead className="text-muted-foreground font-medium">PROPOSER</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {blocks.length === 0 ? (
                <TableRow className="hover:bg-transparent">
                  <TableCell colSpan={5} className="h-24 text-center text-muted-foreground">
                    No blocks available.
                  </TableCell>
                </TableRow>
              ) : (
                blocks.map((block) => (
                  <TableRow 
                    key={block.height} 
                    className="border-border/30 hover:bg-secondary/30 transition-colors"
                  >
                    <TableCell className="font-mono text-frost">#{block.height.toLocaleString()}</TableCell>
                    <TableCell className="text-muted-foreground">{block.time}</TableCell>
                    <TableCell>
                      <span className="px-2 py-1 rounded bg-frost/10 text-frost text-xs font-medium">
                        {block.txs}tx
                      </span>
                    </TableCell>
                    <TableCell>
                      <button 
                        onClick={() => handleCopy(block.hash)}
                        aria-label={`Copy block hash ${block.hash}`}
                        className="flex items-center gap-2 font-mono text-sm text-foreground hover:text-frost transition-colors group"
                      >
                        {block.hash}
                        <Copy className="w-3 h-3 opacity-0 group-hover:opacity-100 transition-opacity" aria-hidden="true" />
                      </button>
                    </TableCell>
                    <TableCell>
                      <button 
                        onClick={() => handleCopy(block.proposer)}
                        aria-label={`Copy proposer address ${block.proposer}`}
                        className="flex items-center gap-2 font-mono text-sm text-muted-foreground hover:text-frost transition-colors group"
                      >
                        {truncateHash(block.proposer, 10)}
                        <Copy className="w-3 h-3 opacity-0 group-hover:opacity-100 transition-opacity" aria-hidden="true" />
                      </button>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </div>
    );
  }
);

LatestBlocksTable.displayName = "LatestBlocksTable";

export default LatestBlocksTable;
