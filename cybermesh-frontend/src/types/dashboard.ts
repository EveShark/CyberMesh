import type {
    DashboardBackendRaw,
    DashboardLedgerRaw,
    DashboardValidatorsRaw,
    NetworkOverviewRaw,
    ConsensusOverviewRaw,
    DashboardBlocksRaw,
    DashboardThreatsRaw,
    DashboardAIRaw
} from "@/lib/api/types/raw";

// Type definition for adapted dashboard data (composed from all sections)
export interface DashboardOverviewData {
    timestamp: number;
    backend?: DashboardBackendRaw;
    ledger?: DashboardLedgerRaw;
    validators?: DashboardValidatorsRaw;
    network?: NetworkOverviewRaw;
    consensus?: ConsensusOverviewRaw;
    blocks?: DashboardBlocksRaw;
    threats?: DashboardThreatsRaw;
    ai?: DashboardAIRaw;
}
