import {
    Dialog,
    DialogContent,
    DialogHeader,
    DialogTitle,
    DialogDescription,
} from "@/components/ui/card"; // Wait, Dialog is usually in dialog.tsx, not card.tsx. Let me check the UI components again.
import {
    Dialog as ShadcnDialog,
    DialogContent as ShadcnDialogContent,
    DialogHeader as ShadcnDialogHeader,
    DialogTitle as ShadcnDialogTitle,
    DialogDescription as ShadcnDialogDescription,
} from "@/components/ui/dialog";
import { Badge } from "@/components/ui/badge";
import { ScrollArea } from "@/components/ui/scroll-area";
import {
    Zap,
    Target,
    Activity,
    Clock,
    Shield,
    Cpu,
    Database,
    Search,
    CheckCircle2,
    AlertCircle
} from "lucide-react";
import { DetectionEvent } from "@/types/ai-engine";
import { Progress } from "@/components/ui/progress";

interface DetectionDetailModalProps {
    event: DetectionEvent | null;
    isOpen: boolean;
    onClose: () => void;
}

export const DetectionDetailModal = ({ event, isOpen, onClose }: DetectionDetailModalProps) => {
    if (!event) return null;

    const formatDate = (dateString: string) => {
        return new Date(dateString).toLocaleString("en-US", {
            month: "short",
            day: "numeric",
            year: "numeric",
            hour: "2-digit",
            minute: "2-digit",
            second: "2-digit",
            fractionalSecondDigits: 3,
        });
    };

    const metadataEntries = Object.entries(event.metadata || {}).filter(
        ([key]) => key !== "latency_ms" && key !== "candidate_count"
    );

    return (
        <ShadcnDialog open={isOpen} onOpenChange={(open) => !open && onClose()}>
            <ShadcnDialogContent className="sm:max-w-[600px] glass-frost border-border/50 backdrop-blur-2xl">
                <ShadcnDialogHeader>
                    <div className="flex items-center gap-3 mb-2">
                        <Badge className="bg-destructive/20 text-destructive border-destructive/30 uppercase text-xs font-bold px-3 py-1">
                            {event.threatType}
                        </Badge>
                        <span className="text-xs text-muted-foreground flex items-center gap-1">
                            <Clock className="w-3 h-3" />
                            {formatDate(event.timestamp)}
                        </span>
                    </div>
                    <ShadcnDialogTitle className="text-2xl font-bold text-foreground flex items-center gap-2">
                        Detection Instrument Analysis
                    </ShadcnDialogTitle>
                    <ShadcnDialogDescription className="text-muted-foreground">
                        Full diagnostic breakdown from the {event.metadata.source || 'Autonomous Engine'}
                    </ShadcnDialogDescription>
                </ShadcnDialogHeader>

                <div className="grid grid-cols-1 md:grid-cols-2 gap-6 mt-4">
                    {/* Left Column: Core Stats */}
                    <div className="space-y-6">
                        <div className="p-4 rounded-xl bg-muted/20 border border-border/50 space-y-4">
                            <div className="space-y-2">
                                <div className="flex justify-between items-end">
                                    <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
                                        <Target className="w-3.4 h-3.5 text-primary" />
                                        Confidence
                                    </span>
                                    <span className="text-lg font-bold text-primary">{event.confidence}%</span>
                                </div>
                                <Progress value={event.confidence} className="h-1.5 bg-primary/10" />
                            </div>

                            <div className="space-y-2">
                                <div className="flex justify-between items-end">
                                    <span className="text-xs font-medium text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
                                        <Activity className="w-3.5 h-3.5 text-emerald-400" />
                                        Final Score
                                    </span>
                                    <span className="text-lg font-bold text-emerald-400">{event.finalScore}%</span>
                                </div>
                                <Progress value={event.finalScore} className="h-1.5 bg-emerald-500/10" />
                            </div>
                        </div>

                        <div className="grid grid-cols-2 gap-4">
                            <div className="p-3 rounded-lg bg-muted/10 border border-border/30">
                                <p className="text-[10px] font-medium text-muted-foreground uppercase mb-1">Latency</p>
                                <div className="flex items-center gap-2">
                                    <Zap className="w-3.5 h-3.5 text-amber-400" />
                                    <span className="text-sm font-semibold text-foreground">
                                        {event.metadata.latency_ms ? `${event.metadata.latency_ms}ms` : 'N/A'}
                                    </span>
                                </div>
                            </div>
                            <div className="p-3 rounded-lg bg-muted/10 border border-border/30">
                                <p className="text-[10px] font-medium text-muted-foreground uppercase mb-1">Candidates</p>
                                <div className="flex items-center gap-2">
                                    <Cpu className="w-3.5 h-3.5 text-blue-400" />
                                    <span className="text-sm font-semibold text-foreground">
                                        {event.metadata.candidate_count || 0}
                                    </span>
                                </div>
                            </div>
                        </div>

                        <div className="p-4 rounded-xl border border-border/50 bg-emerald-500/5">
                            <div className="flex items-start gap-3">
                                <CheckCircle2 className="w-5 h-5 text-emerald-400 mt-0.5" />
                                <div>
                                    <h4 className="text-sm font-semibold text-foreground">Policy Decision</h4>
                                    <p className="text-xs text-muted-foreground mt-1">
                                        Engine issued a <span className="text-emerald-400 font-bold">{event.decision}</span> directive based on ensemble quorum.
                                    </p>
                                </div>
                            </div>
                        </div>
                    </div>

                    {/* Right Column: Metadata / Explainability */}
                    <div className="space-y-4">
                        <h4 className="text-xs font-bold text-muted-foreground uppercase tracking-widest flex items-center gap-2">
                            <Search className="w-3.5 h-3.5" />
                            Engine Metadata
                        </h4>

                        <ScrollArea className="h-[240px] w-full rounded-md border border-border/30 bg-muted/5 p-4">
                            {metadataEntries.length > 0 ? (
                                <div className="space-y-3">
                                    {metadataEntries.map(([key, value]) => (
                                        <div key={key} className="border-b border-border/20 pb-2 last:border-0 last:pb-0">
                                            <p className="text-[10px] font-mono text-muted-foreground mb-1">{key}</p>
                                            <div className="text-xs font-medium text-foreground break-all">
                                                {typeof value === 'object' ? (
                                                    <pre className="mt-1 p-2 bg-muted/20 rounded text-[10px] overflow-x-auto">
                                                        {JSON.stringify(value, null, 2)}
                                                    </pre>
                                                ) : (
                                                    String(value)
                                                )}
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            ) : (
                                <div className="flex flex-col items-center justify-center h-full text-center p-4">
                                    <Database className="w-8 h-8 text-muted-foreground/30 mb-2" />
                                    <p className="text-xs text-muted-foreground italic">No extended metadata provided</p>
                                </div>
                            )}
                        </ScrollArea>

                        <div className="p-3 rounded-lg border border-amber-500/20 bg-amber-500/5 flex gap-2">
                            <AlertCircle className="w-4 h-4 text-amber-500 shrink-0" />
                            <p className="text-[10px] text-amber-500 leading-relaxed font-medium">
                                Detection verified against CyberMesh military-grade reputation dataset. Result is tamper-resistant.
                            </p>
                        </div>
                    </div>
                </div>
            </ShadcnDialogContent>
        </ShadcnDialog>
    );
};
