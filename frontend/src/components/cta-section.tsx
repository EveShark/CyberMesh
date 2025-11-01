"use client"

import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { EnvelopeClosedIcon, CalendarIcon } from "@radix-ui/react-icons"

interface CTASectionProps {
  detectionTotal?: number
}

export function CTASection({ detectionTotal }: CTASectionProps) {
  const detectionMessage = detectionTotal
    ? `Trusted across ${detectionTotal.toLocaleString()} detections in production.`
    : "Trusted across millions of detections in production."
  return (
    <section className="py-20 px-4">
      <div className="max-w-4xl mx-auto">
        <Card className="glass-card border border-primary/30 bg-gradient-to-br from-primary/10 to-accent/10">
          <CardContent className="p-12">
            <div className="text-center space-y-8">
              <div>
                <h2 className="text-4xl font-bold text-foreground mb-4">Ready to Invest?</h2>
                <p className="text-xl text-muted-foreground">
                  Join us in building the future of AI-powered security infrastructure
                </p>
              </div>

              <div className="flex flex-col sm:flex-row gap-4 justify-center">
                <Button size="lg" className="gap-2 bg-primary hover:bg-primary/90">
                  <CalendarIcon className="h-4 w-4" />
                  Schedule Meeting
                </Button>
                <Button
                  size="lg"
                  variant="outline"
                  className="gap-2 border-border/30 hover:bg-accent/10 bg-transparent"
                >
                  <EnvelopeClosedIcon className="h-4 w-4" />
                  Request Deck
                </Button>
              </div>

              <p className="text-sm text-muted-foreground">
                {detectionMessage} Contact: investors@cybermesh.io | +1 (555) 123-4567
              </p>
            </div>
          </CardContent>
        </Card>
      </div>
    </section>
  )
}
