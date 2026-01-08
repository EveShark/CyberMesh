import { BlockSummary } from "@/types/blockchain";
import { Copy } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useToast } from "@/hooks/common/use-toast";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";

interface BlockchainTableProps {
  blocks: BlockSummary[];
  total?: number;
  onBlockSelect?: (block: BlockSummary) => void;
  selectedHeight?: number;
}

const truncateHash = (hash: string, length = 8) => {
  if (hash.length <= length * 2) return hash;
  return `${hash.slice(0, length)}...${hash.slice(-length)}`;
};

const BlockchainTable = ({
  blocks,
  total,
  onBlockSelect,
  selectedHeight
}: BlockchainTableProps) => {
  const { toast } = useToast();

  const handleCopy = (e: React.MouseEvent, text: string) => {
    e.stopPropagation();
    navigator.clipboard.writeText(text);
    toast({
      title: "Copied to clipboard",
      description: text.slice(0, 20) + "...",
    });
  };

  const startIndex = blocks.length > 0 ? blocks[blocks.length - 1]?.height : 0;
  const endIndex = blocks.length > 0 ? blocks[0]?.height : 0;
  const totalCount = total || blocks.length;

  return (
    <div className="glass-frost rounded-lg overflow-hidden min-w-0">
      <div className="p-3 md:p-4 border-b border-border">
        <h3 className="text-base md:text-lg font-semibold text-foreground">Block History</h3>
        <p className="text-xs md:text-sm text-muted-foreground">
          Showing #{startIndex.toLocaleString()} - #{endIndex.toLocaleString()} of ~{totalCount.toLocaleString()}
        </p>
      </div>

      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow className="border-border hover:bg-transparent">
              <TableHead className="text-muted-foreground font-medium text-xs md:text-sm whitespace-nowrap">HEIGHT</TableHead>
              <TableHead className="text-muted-foreground font-medium text-xs md:text-sm whitespace-nowrap">TIME</TableHead>
              <TableHead className="text-muted-foreground font-medium text-xs md:text-sm whitespace-nowrap">TXS</TableHead>
              <TableHead className="text-muted-foreground font-medium text-xs md:text-sm whitespace-nowrap">HASH</TableHead>
              <TableHead className="text-muted-foreground font-medium text-xs md:text-sm whitespace-nowrap hidden sm:table-cell">PROPOSER</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {blocks.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="text-center text-muted-foreground py-8">
                  No blocks available
                </TableCell>
              </TableRow>
            ) : (
              blocks.map((block) => (
                <TableRow
                  key={block.height}
                  onClick={() => onBlockSelect?.(block)}
                  className={`cursor-pointer border-border transition-colors ${selectedHeight === block.height
                      ? "bg-primary/10"
                      : "hover:bg-muted/30"
                    }`}
                >
                  <TableCell className="font-medium text-foreground text-xs md:text-sm whitespace-nowrap">
                    {block.height.toLocaleString()}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs md:text-sm whitespace-nowrap">
                    {block.time}
                  </TableCell>
                  <TableCell className="text-muted-foreground text-xs md:text-sm">
                    {block.txs}
                  </TableCell>
                  <TableCell>
                    <div className="flex items-center gap-1 md:gap-2">
                      <span className="font-mono text-xs md:text-sm text-frost">
                        {truncateHash(block.hash, 4)}
                      </span>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5 md:h-6 md:w-6 shrink-0"
                        onClick={(e) => handleCopy(e, block.hash)}
                      >
                        <Copy className="h-3 w-3" />
                      </Button>
                    </div>
                  </TableCell>
                  <TableCell className="hidden sm:table-cell">
                    <div className="flex items-center gap-1 md:gap-2">
                      <span className="font-mono text-xs md:text-sm text-muted-foreground">
                        {truncateHash(block.proposer, 4)}
                      </span>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-5 w-5 md:h-6 md:w-6 shrink-0"
                        onClick={(e) => handleCopy(e, block.proposer)}
                      >
                        <Copy className="h-3 w-3" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  );
};

export default BlockchainTable;
