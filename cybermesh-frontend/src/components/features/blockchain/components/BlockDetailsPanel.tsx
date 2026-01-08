import { BlockDetail } from "@/types/blockchain";
import { Box, Hash, Clock, ArrowUpDown, HardDrive, Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useToast } from "@/hooks/common/use-toast";

interface BlockDetailsPanelProps {
  block: BlockDetail | null;
  maxTransactionsToShow?: number;
}

const BlockDetailsPanel = ({ block, maxTransactionsToShow = 5 }: BlockDetailsPanelProps) => {
  const { toast } = useToast();

  const handleCopy = (text: string) => {
    navigator.clipboard.writeText(text);
    toast({
      title: "Copied to clipboard",
      description: text.slice(0, 20) + "...",
    });
  };

  if (!block) {
    return (
      <div className="glass-frost rounded-lg p-4 md:p-6">
        <div className="flex items-center gap-2 mb-4">
          <Box className="h-4 w-4 md:h-5 md:w-5 text-frost" />
          <h3 className="text-base md:text-lg font-semibold text-foreground">Block Details</h3>
        </div>
        <div className="h-32 md:h-48 flex items-center justify-center text-sm text-muted-foreground">
          Select a block to view details
        </div>
      </div>
    );
  }

  const remainingTxs = block.transactions.length - maxTransactionsToShow;
  const displayedTxs = block.transactions.slice(0, maxTransactionsToShow);

  return (
    <div className="glass-frost rounded-lg p-4 md:p-6 min-w-0">
      <div className="flex items-center gap-2 mb-4 md:mb-6">
        <Box className="h-4 w-4 md:h-5 md:w-5 text-frost shrink-0" />
        <h3 className="text-base md:text-lg font-semibold text-foreground truncate">
          Block #{block.height.toLocaleString()}
        </h3>
      </div>
      
      <div className="space-y-3 md:space-y-4">
        {/* Hash */}
        <div className="flex items-start gap-2 md:gap-3">
          <Hash className="h-3.5 w-3.5 md:h-4 md:w-4 text-muted-foreground mt-1 shrink-0" />
          <div className="min-w-0 flex-1 overflow-hidden">
            <span className="text-xs text-muted-foreground block">Hash</span>
            <div className="flex items-center gap-1 md:gap-2">
              <span className="text-xs md:text-sm font-mono text-foreground truncate block">
                {block.hash}
              </span>
              <Button
                variant="ghost"
                size="icon"
                className="h-5 w-5 md:h-6 md:w-6 shrink-0"
                onClick={() => handleCopy(block.hash)}
              >
                <Copy className="h-3 w-3" />
              </Button>
            </div>
          </div>
        </div>
        
        {/* Timestamp */}
        <div className="flex items-start gap-2 md:gap-3">
          <Clock className="h-3.5 w-3.5 md:h-4 md:w-4 text-muted-foreground mt-1 shrink-0" />
          <div className="min-w-0">
            <span className="text-xs text-muted-foreground block">Timestamp</span>
            <span className="text-xs md:text-sm font-medium text-foreground">{block.timestamp}</span>
          </div>
        </div>
        
        {/* Transactions Count */}
        <div className="flex items-start gap-2 md:gap-3">
          <ArrowUpDown className="h-3.5 w-3.5 md:h-4 md:w-4 text-muted-foreground mt-1 shrink-0" />
          <div>
            <span className="text-xs text-muted-foreground block">Transactions</span>
            <span className="text-xs md:text-sm font-medium text-foreground">{block.txCount}</span>
          </div>
        </div>
        
        {/* Size */}
        <div className="flex items-start gap-2 md:gap-3">
          <HardDrive className="h-3.5 w-3.5 md:h-4 md:w-4 text-muted-foreground mt-1 shrink-0" />
          <div>
            <span className="text-xs text-muted-foreground block">Size</span>
            <span className="text-xs md:text-sm font-medium text-foreground">{block.size}</span>
          </div>
        </div>
        
        {/* Transactions List */}
        <div className="pt-3 md:pt-4 border-t border-border">
          <span className="text-xs md:text-sm font-medium text-foreground mb-2 md:mb-3 block">Transactions</span>
          <div className="space-y-1.5 md:space-y-2">
            {displayedTxs.map((tx) => (
              <div 
                key={tx.hash}
                className="flex items-center justify-between p-1.5 md:p-2 rounded bg-background/30 text-xs md:text-sm gap-2"
              >
                <span className="font-mono text-muted-foreground truncate flex-1 min-w-0">
                  {tx.hash}
                </span>
                <span className="text-xs text-muted-foreground shrink-0">{tx.size}</span>
              </div>
            ))}
            {remainingTxs > 0 && (
              <div className="text-center text-xs md:text-sm text-muted-foreground py-2">
                +{remainingTxs} more transactions
              </div>
            )}
          </div>
        </div>
      </div>
    </div>
  );
};

export default BlockDetailsPanel;
