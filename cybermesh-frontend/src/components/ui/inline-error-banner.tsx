import { AlertTriangle, RefreshCw, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { useState } from "react";
import { cn } from "@/lib/utils";

interface InlineErrorBannerProps {
  message: string;
  onRetry?: () => void;
  onDismiss?: () => void;
  className?: string;
}

export const InlineErrorBanner = ({
  message,
  onRetry,
  onDismiss,
  className,
}: InlineErrorBannerProps) => {
  const [isDismissed, setIsDismissed] = useState(false);

  if (isDismissed) return null;

  const handleDismiss = () => {
    setIsDismissed(true);
    onDismiss?.();
  };

  return (
    <div
      className={cn(
        "flex items-center justify-between gap-3 rounded-lg border border-yellow-500/50 bg-yellow-500/10 px-4 py-3",
        className
      )}
    >
      <div className="flex items-center gap-3">
        <AlertTriangle className="h-4 w-4 shrink-0 text-yellow-600 dark:text-yellow-400" />
        <span className="text-sm text-yellow-700 dark:text-yellow-300">{message}</span>
      </div>
      <div className="flex items-center gap-2">
        {onRetry && (
          <Button
            onClick={onRetry}
            variant="ghost"
            size="sm"
            className="h-7 px-2 text-yellow-700 hover:bg-yellow-500/20 hover:text-yellow-800 dark:text-yellow-300 dark:hover:text-yellow-200"
          >
            <RefreshCw className="mr-1.5 h-3.5 w-3.5" />
            Retry
          </Button>
        )}
        <Button
          onClick={handleDismiss}
          variant="ghost"
          size="sm"
          className="h-7 w-7 p-0 text-yellow-700 hover:bg-yellow-500/20 hover:text-yellow-800 dark:text-yellow-300 dark:hover:text-yellow-200"
        >
          <X className="h-4 w-4" />
          <span className="sr-only">Dismiss</span>
        </Button>
      </div>
    </div>
  );
};
