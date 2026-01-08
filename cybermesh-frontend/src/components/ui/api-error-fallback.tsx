import { AlertTriangle, RefreshCw, WifiOff, Clock, Copy, CheckCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { ApiError } from "@/lib/api";
import { useState } from "react";
import { toast } from "sonner";

interface ApiErrorFallbackProps {
  error: Error | null;
  onRetry?: () => void;
  title?: string;
}

export const ApiErrorFallback = ({
  error,
  onRetry,
  title = "Failed to load data",
}: ApiErrorFallbackProps) => {
  const [copied, setCopied] = useState(false);

  const isApiError = error instanceof ApiError;
  const isTimeout = isApiError && (error as ApiError).isTimeout;
  const isNetworkError = isApiError 
    ? (error as ApiError).isNetworkError 
    : error?.message.includes("fetch") || error?.message.includes("network");
  
  const statusCode = isApiError ? (error as ApiError).status : null;
  const statusText = isApiError ? (error as ApiError).statusText : null;

  // Get appropriate icon
  const Icon = isTimeout ? Clock : isNetworkError ? WifiOff : AlertTriangle;
  
  // Get user-friendly message
  const getMessage = () => {
    if (isTimeout) {
      return "The request took too long. The server may be under heavy load.";
    }
    if (isNetworkError) {
      return "Unable to connect to the server. Please check your internet connection.";
    }
    if (statusCode && statusCode >= 500) {
      return `Server error (${statusCode}). Our team has been notified.`;
    }
    if (statusCode && statusCode >= 400) {
      return `Request error (${statusCode}): ${statusText || "Bad request"}`;
    }
    return "An unexpected error occurred while fetching data.";
  };

  // Copy error details for support
  const handleCopyError = async () => {
    const errorDetails = {
      message: error?.message,
      status: statusCode,
      statusText,
      isTimeout,
      isNetworkError,
      timestamp: new Date().toISOString(),
      userAgent: navigator.userAgent,
    };

    try {
      await navigator.clipboard.writeText(JSON.stringify(errorDetails, null, 2));
      setCopied(true);
      toast.success("Error details copied to clipboard");
      setTimeout(() => setCopied(false), 2000);
    } catch {
      toast.error("Failed to copy error details");
    }
  };

  return (
    <Card className="border-yellow-500/50 bg-yellow-500/5">
      <CardHeader>
        <CardTitle className="flex items-center gap-2 text-yellow-600 dark:text-yellow-400">
          <Icon className="h-5 w-5" />
          {title}
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        <p className="text-sm text-muted-foreground">
          {getMessage()}
        </p>
        
        {/* Status code badge */}
        {statusCode && statusCode > 0 && (
          <div className="inline-flex items-center gap-2 rounded-md bg-muted px-2 py-1 text-xs font-mono">
            <span className="text-muted-foreground">HTTP</span>
            <span className={statusCode >= 500 ? "text-destructive" : "text-yellow-600"}>
              {statusCode}
            </span>
          </div>
        )}

        <div className="flex flex-wrap gap-2">
          {onRetry && (
            <Button onClick={onRetry} variant="outline" size="sm">
              <RefreshCw className="mr-2 h-4 w-4" />
              Retry
            </Button>
          )}
          <Button 
            onClick={handleCopyError} 
            variant="ghost" 
            size="sm"
            className="text-muted-foreground"
          >
            {copied ? (
              <CheckCircle className="mr-2 h-4 w-4 text-green-500" />
            ) : (
              <Copy className="mr-2 h-4 w-4" />
            )}
            Copy error details
          </Button>
        </div>
      </CardContent>
    </Card>
  );
};
