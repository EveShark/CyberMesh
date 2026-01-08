import { cn } from "@/lib/utils";
import { Skeleton } from "./skeleton";

interface SkeletonCardProps {
  className?: string;
  rows?: number;
}

export const SkeletonCard = ({ className, rows = 3 }: SkeletonCardProps) => (
  <div className={cn("rounded-xl p-5 glass-frost", className)}>
    <div className="flex items-start justify-between mb-4">
      <div className="flex items-center gap-3">
        <Skeleton className="h-10 w-10 rounded-lg" />
        <div className="space-y-2">
          <Skeleton className="h-4 w-24" />
          <Skeleton className="h-3 w-16" />
        </div>
      </div>
      <Skeleton className="h-6 w-16 rounded-full" />
    </div>
    <div className="grid grid-cols-2 gap-3">
      {Array.from({ length: rows * 2 }).map((_, i) => (
        <div key={i} className="space-y-1">
          <Skeleton className="h-3 w-12" />
          <Skeleton className="h-4 w-16" />
        </div>
      ))}
    </div>
  </div>
);

interface SkeletonTableProps {
  className?: string;
  rows?: number;
  columns?: number;
}

export const SkeletonTable = ({ className, rows = 5, columns = 5 }: SkeletonTableProps) => (
  <div className={cn("rounded-xl p-5 glass-frost", className)}>
    <div className="flex items-center gap-3 mb-4">
      <Skeleton className="h-10 w-10 rounded-lg" />
      <div className="space-y-2">
        <Skeleton className="h-4 w-32" />
        <Skeleton className="h-3 w-24" />
      </div>
    </div>
    <div className="space-y-3">
      {/* Table header */}
      <div className="grid gap-4" style={{ gridTemplateColumns: `repeat(${columns}, 1fr)` }}>
        {Array.from({ length: columns }).map((_, i) => (
          <Skeleton key={i} className="h-4 w-full" />
        ))}
      </div>
      {/* Table rows */}
      {Array.from({ length: rows }).map((_, i) => (
        <div key={i} className="grid gap-4" style={{ gridTemplateColumns: `repeat(${columns}, 1fr)` }}>
          {Array.from({ length: columns }).map((_, j) => (
            <Skeleton key={j} className="h-6 w-full" />
          ))}
        </div>
      ))}
    </div>
  </div>
);

interface SkeletonChartProps {
  className?: string;
  bars?: number;
}

export const SkeletonChart = ({ className, bars = 8 }: SkeletonChartProps) => (
  <div className={cn("rounded-xl p-5 glass-frost", className)}>
    <div className="flex items-center gap-3 mb-4">
      <Skeleton className="h-10 w-10 rounded-lg" />
      <div className="space-y-2">
        <Skeleton className="h-4 w-32" />
        <Skeleton className="h-3 w-48" />
      </div>
    </div>
    <div className="h-48 flex items-end gap-2">
      {Array.from({ length: bars }).map((_, i) => (
        <Skeleton 
          key={i} 
          className="flex-1 rounded-t" 
          style={{ height: `${30 + (i % 3) * 20 + (i % 2) * 15}%` }}
        />
      ))}
    </div>
  </div>
);

interface SkeletonMetricProps {
  className?: string;
}

export const SkeletonMetric = ({ className }: SkeletonMetricProps) => (
  <div className={cn("rounded-xl p-4 glass-frost", className)}>
    <div className="flex items-center gap-3 mb-3">
      <Skeleton className="h-8 w-8 rounded-lg" />
      <Skeleton className="h-4 w-20" />
    </div>
    <Skeleton className="h-8 w-24 mb-2" />
    <Skeleton className="h-3 w-16" />
  </div>
);
