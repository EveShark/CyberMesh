package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

type safeModeToggleResult struct {
	Enabled bool `json:"enabled"`
}

type killSwitchToggleResult struct {
	Enabled bool `json:"enabled"`
}

func (r *commandRunner) renderAnomalies(resp anomalyListResponse) error {
	if len(resp.Anomalies) == 0 {
		_, _ = fmt.Fprintln(r.out, "No anomalies found.")
		return nil
	}
	renderTitle(r.out, "Anomalies")
	_, _ = fmt.Fprintf(r.out, "Count: %d\n\n", resp.Count)
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "ID\tSEVERITY\tTYPE\tSOURCE\tCONF\tBLOCK\tWHEN")
	for _, item := range resp.Anomalies {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%.2f\t%d\t%s\n",
			shortID(item.ID, 20),
			renderStatus(item.Severity),
			blankDash(item.Type),
			blankDash(item.Source),
			item.Confidence,
			item.BlockHeight,
			formatTimestampAuto(item.Timestamp),
		)
	}
	return tw.Flush()
}

func (r *commandRunner) renderPolicies(resp policyListResult) error {
	if len(resp.Rows) == 0 {
		_, _ = fmt.Fprintln(r.out, "No policies found.")
		return nil
	}
	renderTitle(r.out, "Policies")
	_, _ = fmt.Fprintf(r.out, "Matches: %d\n\n", len(resp.Rows))
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "POLICY\tSTATUS\tTRACE\tWORKFLOW\tOUTBOX\tACKS\tWHEN")
	for _, row := range resp.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\t%s\n",
			shortID(row.PolicyID, 18),
			renderStatus(row.LatestStatus),
			shortID(row.TraceID, 20),
			blankDash(shortID(row.WorkflowID, 18)),
			row.OutboxCount,
			row.AckCount,
			formatTimestampAuto(row.LatestCreatedAt),
		)
	}
	return tw.Flush()
}

func (r *commandRunner) renderPolicyDetail(resp policyDetailResult) error {
	renderTitle(r.out, "Policy")
	renderKV(r.out, map[string]string{
		"Policy ID":    blankDash(resp.Summary.PolicyID),
		"Status":       renderStatus(resp.Summary.LatestStatus),
		"Trace ID":     blankDash(resp.Summary.TraceID),
		"Workflow ID":  blankDash(resp.Summary.WorkflowID),
		"Request ID":   blankDash(resp.Summary.RequestID),
		"Command ID":   blankDash(resp.Summary.CommandID),
		"Ack Event ID": blankDash(resp.Summary.LatestAckEventID),
		"Ack Result":   blankDash(renderStatus(resp.Summary.LatestAckResult)),
		"Source Type":  blankDash(resp.Summary.SourceType),
		"Source ID":    blankDash(resp.Summary.SourceID),
		"Scope":        blankDash(resp.Summary.ScopeIdentifier),
	})
	return r.renderTrace(resp.Trace)
}

func (r *commandRunner) renderPolicyCoverage(resp policyCoverageResult) error {
	renderTitle(r.out, "Policy Coverage")
	renderKV(r.out, map[string]string{
		"Policy ID":              blankDash(resp.PolicyID),
		"Outbox Rows":            fmt.Sprintf("%d", resp.OutboxCount),
		"ACK History Rows":       fmt.Sprintf("%d", resp.AckHistoryCount),
		"Pending":                fmt.Sprintf("%d", resp.PendingCount),
		"Publishing":             fmt.Sprintf("%d", resp.PublishingCount),
		"Published":              fmt.Sprintf("%d", resp.PublishedCount),
		"Retry":                  fmt.Sprintf("%d", resp.RetryCount),
		"Terminal Failed":        fmt.Sprintf("%d", resp.TerminalFailedCount),
		"Acked":                  fmt.Sprintf("%d", resp.AckedCount),
		"Latest ACK Event":       blankDash(resp.LatestAckEventID),
		"Latest ACK Result":      blankDash(renderStatus(resp.LatestAckResult)),
		"Latest ACK Controller":  blankDash(resp.LatestAckController),
		"Latest ACKed At":        formatTimestampAuto(resp.LatestAckedAt),
		"Publish Coverage Ratio": fmt.Sprintf("%.2f", resp.PublishCoverageRatio),
		"ACK Coverage Ratio":     fmt.Sprintf("%.2f", resp.AckCoverageRatio),
	})
	return nil
}

func (r *commandRunner) renderPolicyAcks(resp policyAcksResult) error {
	renderTitle(r.out, "Policy ACKs")
	renderKV(r.out, map[string]string{
		"Policy ID": blankDash(resp.PolicyID),
		"ACK Count": fmt.Sprintf("%d", resp.Count),
	})
	if len(resp.Acks) == 0 {
		_, _ = fmt.Fprintln(r.out, "\nNo ACK rows found for this policy.")
		return nil
	}
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "ACK_EVENT\tRESULT\tCONTROLLER\tREQUEST\tCOMMAND\tWORKFLOW\tWHEN")
	for _, row := range resp.Acks {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			shortID(row.AckEventID, 18),
			renderStatus(row.Result),
			blankDash(row.ControllerInstance),
			shortID(row.RequestID, 18),
			shortID(row.CommandID, 18),
			shortID(row.WorkflowID, 18),
			formatTimestampAuto(row.ObservedAt),
		)
	}
	return tw.Flush()
}

func (r *commandRunner) renderAckRows(resp ackListResponse) error {
	if len(resp.Rows) == 0 {
		_, _ = fmt.Fprintln(r.out, "No ACK rows found.")
		return nil
	}
	renderTitle(r.out, "ACK Rows")
	_, _ = fmt.Fprintf(r.out, "Matches: %d\n\n", len(resp.Rows))
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "POLICY\tRESULT\tCONTROLLER\tSCOPE\tACK_EVENT\tWHEN\tWORKFLOW")
	for _, row := range resp.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			shortID(row.PolicyID, 18),
			renderStatus(row.Result),
			blankDash(row.ControllerInstance),
			blankDash(row.ScopeIdentifier),
			shortID(row.AckEventID, 18),
			formatTimestampAuto(row.ObservedAt),
			blankDash(shortID(row.WorkflowID, 18)),
		)
	}
	return tw.Flush()
}

func (r *commandRunner) renderValidators(resp validatorListResponse) error {
	renderTitle(r.out, "Validators")
	_, _ = fmt.Fprintf(r.out, "Total: %d\n\n", resp.Total)
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "VALIDATOR_ID\tSTATUS\tPUBLIC_KEY")
	for _, item := range resp.Validators {
		fmt.Fprintf(tw, "%s\t%s\t%s\n",
			shortID(item.ID, 22),
			renderStatus(item.Status),
			shortID(item.PublicKey, 24),
		)
	}
	return tw.Flush()
}

func (r *commandRunner) renderConsensus(resp consensusOverview) error {
	renderTitle(r.out, "Consensus")
	renderKV(r.out, map[string]string{
		"Leader":       blankDash(shortID(resp.Leader, 20)),
		"Leader ID":    blankDash(shortID(resp.LeaderID, 20)),
		"Term":         fmt.Sprintf("%d", resp.Term),
		"Phase":        renderStatus(resp.Phase),
		"Active Peers": fmt.Sprintf("%d", resp.ActivePeers),
		"Quorum Size":  fmt.Sprintf("%d", resp.QuorumSize),
		"Updated At":   blankDash(resp.UpdatedAt),
	})
	if len(resp.Proposals) > 0 {
		renderSubTitle(r.out, "Recent Proposals")
		tw := newTabWriter(r.out)
		fmt.Fprintln(tw, "BLOCK\tVIEW\tPROPOSER\tWHEN")
		for _, p := range resp.Proposals {
			fmt.Fprintf(tw, "%d\t%d\t%s\t%s\n",
				p.Block,
				p.View,
				shortID(p.Proposer, 20),
				formatTimestampAuto(p.Timestamp),
			)
		}
		_ = tw.Flush()
	}
	if len(resp.SuspiciousNodes) > 0 {
		renderSubTitle(r.out, "Suspicious Nodes")
		tw := newTabWriter(r.out)
		fmt.Fprintln(tw, "ID\tSTATUS\tUPTIME\tSCORE\tREASON")
		for _, n := range resp.SuspiciousNodes {
			fmt.Fprintf(tw, "%s\t%s\t%.2f\t%.2f\t%s\n",
				shortID(n.ID, 20),
				renderStatus(n.Status),
				n.Uptime,
				n.SuspicionScore,
				blankDash(n.Reason),
			)
		}
		return tw.Flush()
	}
	return nil
}

func (r *commandRunner) renderBacklog(resp outboxBacklogResult) error {
	renderTitle(r.out, "Outbox Backlog")
	renderKV(r.out, map[string]string{
		"Pending":            fmt.Sprintf("%d", resp.Pending),
		"Retry":              fmt.Sprintf("%d", resp.Retry),
		"Publishing":         fmt.Sprintf("%d", resp.Publishing),
		"Published Rows":     fmt.Sprintf("%d", resp.PublishedRows),
		"Acked Rows":         fmt.Sprintf("%d", resp.AckedRows),
		"Terminal Rows":      fmt.Sprintf("%d", resp.TerminalRows),
		"Total Rows":         fmt.Sprintf("%d", resp.TotalRows),
		"Oldest Pending Age": formatDurationMillis(resp.OldestPendingAgeMs),
		"ACK Closure Ratio":  fmt.Sprintf("%.2f", resp.AckClosureRatio),
	})
	return nil
}

func (r *commandRunner) renderAIHistory(resp aiHistoryResult) error {
	renderTitle(r.out, "AI History")
	renderKV(r.out, map[string]string{
		"Count":      fmt.Sprintf("%d", resp.Count),
		"Updated At": blankDash(resp.UpdatedAt),
	})
	if len(resp.Detections) == 0 {
		_, _ = fmt.Fprintln(r.out, "\nNo AI detections found.")
		return nil
	}
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "ID\tSEVERITY\tTYPE\tCONF\tSOURCE\tPOLICY\tWORKFLOW\tWHEN")
	for _, row := range resp.Detections {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%.2f\t%s\t%s\t%s\t%s\n",
			shortID(row.ID, 18),
			renderStatus(row.Severity),
			blankDash(row.Type),
			row.Confidence,
			blankDash(shortID(row.Source, 16)),
			shortID(row.PolicyID, 18),
			shortID(row.WorkflowID, 18),
			formatTimestampAuto(row.Timestamp),
		)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	renderSubTitle(r.out, "Detection Details")
	for _, row := range resp.Detections {
		_, _ = fmt.Fprintln(r.out, kvLine("Detection", shortID(row.ID, 18)))
		_, _ = fmt.Fprintln(r.out, kvLine("Title", row.Title))
		_, _ = fmt.Fprintln(r.out, kvLine("Description", row.Description))
		_, _ = fmt.Fprintln(r.out, kvLine("Trace", row.TraceID))
		_, _ = fmt.Fprintln(r.out, kvLine("Anomaly", row.AnomalyID))
		_, _ = fmt.Fprintln(r.out, kvLine("Sentinel Event", row.SentinelEventID))
		_, _ = fmt.Fprintln(r.out)
	}
	return nil
}

func (r *commandRunner) renderAISuspiciousNodes(resp aiSuspiciousNodesResult) error {
	renderTitle(r.out, "AI Suspicious Nodes")
	renderKV(r.out, map[string]string{
		"Count":      fmt.Sprintf("%d", len(resp.Nodes)),
		"Updated At": blankDash(resp.UpdatedAt),
	})
	if len(resp.Nodes) == 0 {
		_, _ = fmt.Fprintln(r.out, "\nNo suspicious nodes reported.")
		return nil
	}
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "ID\tSTATUS\tUPTIME\tSCORE\tREASON")
	for _, row := range resp.Nodes {
		fmt.Fprintf(tw, "%s\t%s\t%.2f\t%.2f\t%s\n",
			shortID(row.ID, 20),
			renderStatus(row.Status),
			row.Uptime,
			row.SuspicionScore,
			blankDash(row.Reason),
		)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	renderSubTitle(r.out, "Node Details")
	for _, row := range resp.Nodes {
		_, _ = fmt.Fprintln(r.out, kvLine("Node", row.ID))
		_, _ = fmt.Fprintln(r.out, kvLine("Status", renderStatus(row.Status)))
		_, _ = fmt.Fprintln(r.out, kvLine("Uptime", fmt.Sprintf("%.2f", row.Uptime)))
		_, _ = fmt.Fprintln(r.out, kvLine("Suspicion Score", fmt.Sprintf("%.2f", row.SuspicionScore)))
		_, _ = fmt.Fprintln(r.out, kvLine("Reason", row.Reason))
		_, _ = fmt.Fprintln(r.out)
	}
	return nil
}

func (r *commandRunner) renderSafeModeResult(resp safeModeToggleResult) error {
	renderTitle(r.out, "Safe Mode")
	state := "disabled"
	if resp.Enabled {
		state = "enabled"
	}
	renderKV(r.out, map[string]string{
		"State": renderStatus(state),
	})
	return nil
}

func (r *commandRunner) renderKillSwitchResult(resp killSwitchToggleResult) error {
	renderTitle(r.out, "Kill Switch")
	state := "disabled"
	if resp.Enabled {
		state = "enabled"
	}
	renderKV(r.out, map[string]string{
		"State": renderStatus(state),
	})
	return nil
}

func (r *commandRunner) renderDoctor(resp doctorResult) error {
	renderTitle(r.out, "Doctor")
	_, _ = fmt.Fprintf(r.out, "Version: %s\n\n", resp.Version)
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "CHECK\tSTATUS\tDETAILS")
	for _, check := range resp.Checks {
		fmt.Fprintf(tw, "%s\t%s\t%s\n",
			blankDash(check.Name),
			renderStatus(check.Status),
			blankDash(check.Details),
		)
	}
	return tw.Flush()
}

func (r *commandRunner) renderControlStatus(resp controlStatusResponse) error {
	renderTitle(r.out, "Control")
	renderKV(r.out, map[string]string{
		"Safe Mode":   renderBooleanStatus(resp.ControlMutationSafeMode),
		"Kill Switch": renderBooleanStatus(resp.ControlMutationKillSwitch),
		"Lease Rows":  fmt.Sprintf("%d", len(resp.Leases)),
	})
	if len(resp.Leases) == 0 {
		return nil
	}
	renderSubTitle(r.out, "Dispatcher Leases")
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "LEASE\tHOLDER\tEPOCH\tACTIVE\tLEASE_UNTIL\tUPDATED_AT")
	for _, row := range resp.Leases {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
			blankDash(shortID(row.LeaseKey, 24)),
			blankDash(shortID(row.HolderID, 24)),
			row.Epoch,
			renderBooleanStatus(row.IsActive),
			formatTimestampAuto(row.LeaseUntil),
			formatTimestampAuto(row.UpdatedAt),
		)
	}
	return tw.Flush()
}

func (r *commandRunner) renderMutationResult(resp controlMutationResponse) error {
	renderTitle(r.out, "Mutation Result")
	renderKV(r.out, map[string]string{
		"Action":      blankDash(resp.ActionType),
		"Action ID":   blankDash(resp.ActionID),
		"Command ID":  blankDash(resp.CommandID),
		"Workflow ID": blankDash(resp.WorkflowID),
		"Outbox ID":   blankDash(resp.Outbox.ID),
		"Policy ID":   blankDash(resp.Outbox.PolicyID),
		"Status":      renderStatus(resp.Outbox.Status),
	})
	return nil
}

func (r *commandRunner) renderWorkflowRollback(resp workflowRollbackResult) error {
	renderTitle(r.out, "Workflow Rollback")
	renderKV(r.out, map[string]string{
		"Action":            blankDash(resp.ActionType),
		"Action ID":         blankDash(resp.ActionID),
		"Command ID":        blankDash(resp.CommandID),
		"Workflow ID":       blankDash(resp.WorkflowID),
		"Affected Policies": fmt.Sprintf("%d", resp.AffectedPolicies),
		"Replay":            fmt.Sprintf("%t", resp.IdempotentReplay),
	})
	if len(resp.Outbox) == 0 {
		return nil
	}
	renderSubTitle(r.out, "Created Rows")
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "OUTBOX\tPOLICY\tSTATUS\tTRACE\tWHEN")
	for _, row := range resp.Outbox {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			shortID(row.ID, 18),
			shortID(row.PolicyID, 18),
			renderStatus(row.Status),
			shortID(row.TraceID, 20),
			formatTimestampAuto(row.CreatedAt),
		)
	}
	return tw.Flush()
}

func (r *commandRunner) renderOutboxRows(resp outboxListResponse) error {
	if len(resp.Rows) == 0 {
		_, _ = fmt.Fprintln(r.out, "No outbox rows found.")
		return nil
	}
	renderTitle(r.out, "Outbox Rows")
	_, _ = fmt.Fprintf(r.out, "Matches: %d\n\n", len(resp.Rows))
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "OUTBOX\tPOLICY\tSTATUS\tSOURCE\tTYPE\tTRACE\tWHEN\tWORKFLOW")
	for _, row := range resp.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			shortID(row.ID, 18),
			shortID(row.PolicyID, 18),
			renderStatus(row.Status),
			blankDash(shortID(row.SourceID, 18)),
			blankDash(row.SourceType),
			shortID(row.TraceID, 20),
			formatTimestampAuto(row.CreatedAt),
			blankDash(shortID(row.WorkflowID, 18)),
		)
	}
	return tw.Flush()
}

func (r *commandRunner) renderTrace(resp traceResponse) error {
	if resp.PolicyID == "" && len(resp.Outbox) == 0 && len(resp.Acks) == 0 {
		_, _ = fmt.Fprintln(r.out, "No trace data found.")
		return nil
	}
	renderTitle(r.out, "Trace")
	renderKV(r.out, map[string]string{
		"Policy ID":         blankDash(resp.PolicyID),
		"Trace ID":          blankDash(resp.TraceID),
		"Source Event ID":   blankDash(resp.SourceEventID),
		"Sentinel Event ID": blankDash(resp.SentinelEventID),
		"Outbox Rows":       fmt.Sprintf("%d", len(resp.Outbox)),
		"ACK Rows":          fmt.Sprintf("%d", len(resp.Acks)),
	})
	if resp.Materialized != nil {
		renderSubTitle(r.out, "Materialized")
		first := "-"
		if resp.Materialized.FirstPolicyAck != nil {
			first = fmt.Sprintf("%s via %s", renderStatus(resp.Materialized.FirstPolicyAck.Result), blankDash(resp.Materialized.FirstPolicyAck.ControllerInstance))
		}
		latest := "-"
		if resp.Materialized.LatestPolicyAck != nil {
			latest = fmt.Sprintf("%s via %s", renderStatus(resp.Materialized.LatestPolicyAck.Result), blankDash(resp.Materialized.LatestPolicyAck.ControllerInstance))
		}
		renderKV(r.out, map[string]string{
			"First ACK":  first,
			"Latest ACK": latest,
		})
	}
	if len(resp.Outbox) > 0 {
		renderSubTitle(r.out, "Outbox")
		tw := newTabWriter(r.out)
		fmt.Fprintln(tw, "OUTBOX\tSTATUS\tTRACE\tREQUEST\tCOMMAND\tWORKFLOW")
		for _, row := range resp.Outbox {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				shortID(row.ID, 18),
				renderStatus(row.Status),
				shortID(row.TraceID, 18),
				shortID(row.RequestID, 18),
				shortID(row.CommandID, 18),
				shortID(row.WorkflowID, 18),
			)
		}
		_ = tw.Flush()
	}
	if len(resp.Acks) > 0 {
		renderSubTitle(r.out, "ACK History")
		tw := newTabWriter(r.out)
		fmt.Fprintln(tw, "ACK_EVENT\tRESULT\tCONTROLLER\tSCOPE\tREQUEST\tCOMMAND\tWORKFLOW")
		for _, row := range resp.Acks {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				shortID(row.AckEventID, 18),
				renderStatus(row.Result),
				blankDash(row.ControllerInstance),
				blankDash(row.ScopeIdentifier),
				shortID(row.RequestID, 18),
				shortID(row.CommandID, 18),
				shortID(row.WorkflowID, 18),
			)
		}
		return tw.Flush()
	}
	return nil
}

func (r *commandRunner) renderWorkflows(resp workflowListResult) error {
	if len(resp.Rows) == 0 {
		_, _ = fmt.Fprintln(r.out, "No workflows found.")
		return nil
	}
	renderTitle(r.out, "Workflows")
	_, _ = fmt.Fprintf(r.out, "Matches: %d\n\n", len(resp.Rows))
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "WORKFLOW\tSTATUS\tPOLICIES\tOUTBOX\tACKS\tTRACE\tWHEN")
	for _, row := range resp.Rows {
		fmt.Fprintf(tw, "%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
			blankDash(shortID(row.WorkflowID, 20)),
			renderStatus(row.LatestStatus),
			row.PolicyCount,
			row.OutboxCount,
			row.AckCount,
			shortID(row.LatestTraceID, 20),
			formatTimestampAuto(row.LatestCreatedAt),
		)
	}
	return tw.Flush()
}

func (r *commandRunner) renderWorkflowDetail(resp workflowDetailResult) error {
	renderTitle(r.out, "Workflow")
	renderKV(r.out, map[string]string{
		"Workflow ID":       blankDash(resp.Summary.WorkflowID),
		"Request ID":        blankDash(resp.Summary.RequestID),
		"Command ID":        blankDash(resp.Summary.CommandID),
		"Latest Policy":     blankDash(resp.Summary.LatestPolicyID),
		"Latest Status":     renderStatus(resp.Summary.LatestStatus),
		"Latest Trace":      blankDash(resp.Summary.LatestTraceID),
		"Policy Count":      fmt.Sprintf("%d", resp.Summary.PolicyCount),
		"Outbox Count":      fmt.Sprintf("%d", resp.Summary.OutboxCount),
		"Ack Count":         fmt.Sprintf("%d", resp.Summary.AckCount),
		"Latest ACK":        blankDash(resp.Summary.LatestAckEventID),
		"Latest ACK Result": blankDash(renderStatus(resp.Summary.LatestAckResult)),
	})
	if len(resp.Policies) > 0 {
		renderSubTitle(r.out, "Policies")
		tw := newTabWriter(r.out)
		fmt.Fprintln(tw, "POLICY\tSTATUS\tTRACE\tOUTBOX\tACKS")
		for _, row := range resp.Policies {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%d\n",
				shortID(row.PolicyID, 18),
				renderStatus(row.LatestStatus),
				shortID(row.TraceID, 20),
				row.OutboxCount,
				row.AckCount,
			)
		}
		_ = tw.Flush()
	}
	if len(resp.Outbox) > 0 {
		renderSubTitle(r.out, "Outbox")
		tw := newTabWriter(r.out)
		fmt.Fprintln(tw, "OUTBOX\tPOLICY\tSTATUS\tTRACE\tWHEN")
		for _, row := range resp.Outbox {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				shortID(row.ID, 18),
				shortID(row.PolicyID, 18),
				renderStatus(row.Status),
				shortID(row.TraceID, 20),
				formatTimestampAuto(row.CreatedAt),
			)
		}
		_ = tw.Flush()
	}
	if len(resp.Acks) > 0 {
		renderSubTitle(r.out, "ACKs")
		tw := newTabWriter(r.out)
		fmt.Fprintln(tw, "POLICY\tACK_EVENT\tRESULT\tCONTROLLER\tWHEN")
		for _, row := range resp.Acks {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				shortID(row.PolicyID, 18),
				shortID(row.AckEventID, 18),
				renderStatus(row.Result),
				blankDash(row.ControllerInstance),
				formatTimestampAuto(row.ObservedAt),
			)
		}
		return tw.Flush()
	}
	return nil
}

func (r *commandRunner) renderAudit(rows []auditEntry) error {
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(r.out, "No audit rows found.")
		return nil
	}
	renderTitle(r.out, "Audit")
	_, _ = fmt.Fprintf(r.out, "Matches: %d\n\n", len(rows))
	tw := newTabWriter(r.out)
	fmt.Fprintln(tw, "ACTION\tTYPE\tTARGET\tPOLICY\tWORKFLOW\tACTOR\tWHEN")
	for _, row := range rows {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			shortID(row.ActionID, 18),
			blankDash(row.ActionType),
			blankDash(row.TargetKind),
			shortID(row.PolicyID, 18),
			shortID(row.WorkflowID, 18),
			blankDash(shortID(row.Actor, 22)),
			formatTimestampAuto(row.CreatedAt),
		)
	}
	return tw.Flush()
}

func (r *commandRunner) renderGenericMap(v any) error {
	flat := flattenMap(v)
	keys := make([]string, 0, len(flat))
	for key := range flat {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tw := newTabWriter(r.out)
	for _, key := range keys {
		fmt.Fprintf(tw, "%s\t%v\n", key, flat[key])
	}
	return tw.Flush()
}

func renderTitle(w io.Writer, title string) {
	_, _ = fmt.Fprintf(w, "%s\n%s\n", title, strings.Repeat("=", len(title)))
}

func renderSubTitle(w io.Writer, title string) {
	_, _ = fmt.Fprintf(w, "\n%s\n", title)
}

func renderKV(w io.Writer, values map[string]string) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	tw := newTabWriter(w)
	for _, key := range keys {
		fmt.Fprintf(tw, "%s\t%s\n", key, values[key])
	}
	_ = tw.Flush()
}

func renderStatus(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "-"
	}
	return strings.ToUpper(raw)
}

func blankDash(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return "-"
	}
	return raw
}

func shortID(raw string, max int) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || max <= 0 || len(raw) <= max {
		return raw
	}
	if max < 9 {
		return raw[:max]
	}
	head := (max - 3) / 2
	tail := max - 3 - head
	return raw[:head] + "..." + raw[len(raw)-tail:]
}

func formatTimestampAuto(ts int64) string {
	if ts <= 0 {
		return "-"
	}
	switch {
	case ts > 1_000_000_000_000:
		return time.UnixMilli(ts).UTC().Format("2006-01-02 15:04:05Z")
	case ts > 1_000_000_000:
		return time.Unix(ts, 0).UTC().Format("2006-01-02 15:04:05Z")
	default:
		return fmt.Sprintf("%d", ts)
	}
}

func formatDurationMillis(ms int64) string {
	if ms <= 0 {
		return "0s"
	}
	return (time.Duration(ms) * time.Millisecond).String()
}

func newTabWriter(w io.Writer) *tabwriter.Writer {
	return tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
}
