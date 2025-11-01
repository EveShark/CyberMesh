"use client"

import { Card, CardContent, CardHeader } from "@/components/ui/card"

export function ThreatLoadingSkeleton() {
  return (
    <div className="space-y-8 animate-pulse">
      {/* Hero Metrics Skeleton */}
      <div className="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-4 gap-4">
        {[1, 2, 3, 4].map((i) => (
          <Card key={i} className="glass-card border-white/10">
            <CardContent className="p-6">
              <div className="flex items-start justify-between mb-4">
                <div className="h-12 w-12 bg-white/5 rounded-lg" />
                <div className="h-8 w-20 bg-white/5 rounded" />
              </div>
              <div className="space-y-2">
                <div className="h-4 w-24 bg-white/5 rounded" />
                <div className="h-3 w-32 bg-white/5 rounded" />
              </div>
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Charts Skeleton */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {[1, 2].map((i) => (
          <Card key={i} className="glass-card border-white/10">
            <CardHeader>
              <div className="h-6 w-48 bg-white/5 rounded" />
            </CardHeader>
            <CardContent>
              <div className="h-64 bg-white/5 rounded" />
            </CardContent>
          </Card>
        ))}
      </div>

      {/* Table Skeleton */}
      <Card className="glass-card border-white/10">
        <CardHeader>
          <div className="h-6 w-40 bg-white/5 rounded" />
        </CardHeader>
        <CardContent>
          <div className="space-y-3">
            {[1, 2, 3, 4, 5].map((i) => (
              <div key={i} className="flex items-center gap-4">
                <div className="h-12 w-full bg-white/5 rounded" />
              </div>
            ))}
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
