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
	renderAnomalySummary(r.out, buildAnomalyListView(resp))
	renderSuggestions(r.out, anomalySuggestions(resp))
	return nil
}

type anomalyListView struct {
	SummaryFields []detailField
	Timeline      []policyTimelineEntry
	Meaning       []string
	Recent        []anomalyRecentEntry
}

type anomalyRecentEntry struct {
	ID         string
	Severity   string
	Type       string
	Source     string
	Confidence string
	Block      string
}

func buildAnomalyListView(resp anomalyListResponse) anomalyListView {
	view := anomalyListView{
		SummaryFields: []detailField{
			{Key: "Count", Value: fmt.Sprintf("%d", resp.Count)},
			{Key: "Severities", Value: fmt.Sprintf("%d", countDistinctAnomalies(resp.Anomalies, func(item struct {
				ID          string  `json:"id"`
				Type        string  `json:"type"`
				Severity    string  `json:"severity"`
				Title       string  `json:"title"`
				Description string  `json:"description"`
				Source      string  `json:"source"`
				BlockHeight uint64  `json:"block_height"`
				Timestamp   int64   `json:"timestamp"`
				Confidence  float64 `json:"confidence"`
				TxHash      string  `json:"tx_hash"`
			}) string { return item.Severity }))},
			{Key: "Types", Value: fmt.Sprintf("%d", countDistinctAnomalies(resp.Anomalies, func(item struct {
				ID          string  `json:"id"`
				Type        string  `json:"type"`
				Severity    string  `json:"severity"`
				Title       string  `json:"title"`
				Description string  `json:"description"`
				Source      string  `json:"source"`
				BlockHeight uint64  `json:"block_height"`
				Timestamp   int64   `json:"timestamp"`
				Confidence  float64 `json:"confidence"`
				TxHash      string  `json:"tx_hash"`
			}) string { return item.Type }))},
			{Key: "Sources", Value: fmt.Sprintf("%d", countDistinctAnomalies(resp.Anomalies, func(item struct {
				ID          string  `json:"id"`
				Type        string  `json:"type"`
				Severity    string  `json:"severity"`
				Title       string  `json:"title"`
				Description string  `json:"description"`
				Source      string  `json:"source"`
				BlockHeight uint64  `json:"block_height"`
				Timestamp   int64   `json:"timestamp"`
				Confidence  float64 `json:"confidence"`
				TxHash      string  `json:"tx_hash"`
			}) string { return item.Source }))},
			{Key: "Latest Seen", Value: formatTimestampAuto(latestAnomalyTimestamp(resp.Anomalies))},
		},
		Timeline: buildAnomalyTimeline(resp.Anomalies),
		Meaning:  buildAnomalyMeaning(resp),
		Recent:   buildAnomalyRecentEntries(resp.Anomalies),
	}
	return view
}

func renderAnomalySummary(w io.Writer, view anomalyListView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			_, _ = fmt.Fprintf(w, "- %s  %s\n", formatTimestampAuto(item.When), item.Label)
		}
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, line := range view.Meaning {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", line)
		}
	}
	if len(view.Recent) > 0 {
		renderSubTitle(w, "Recent Anomalies")
		for i, row := range view.Recent {
			if i >= 5 {
				break
			}
			_, _ = fmt.Fprintln(w, detailLine("ID", row.ID))
			_, _ = fmt.Fprintln(w, detailLine("Severity", row.Severity))
			_, _ = fmt.Fprintln(w, detailLine("Type", row.Type))
			_, _ = fmt.Fprintln(w, detailLine("Source", row.Source))
			_, _ = fmt.Fprintln(w, detailLine("Confidence", row.Confidence))
			_, _ = fmt.Fprintln(w, detailLine("Block", row.Block))
			_, _ = fmt.Fprintln(w)
		}
	}
}

func buildAnomalyTimeline(rows []struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	BlockHeight uint64  `json:"block_height"`
	Timestamp   int64   `json:"timestamp"`
	Confidence  float64 `json:"confidence"`
	TxHash      string  `json:"tx_hash"`
}) []policyTimelineEntry {
	entries := make([]policyTimelineEntry, 0, len(rows))
	for _, row := range rows {
		label := joinParts(strings.ToLower(strings.TrimSpace(row.Severity)), strings.ToLower(strings.TrimSpace(row.Type)), strings.ToLower(strings.TrimSpace(row.Source)))
		if label == "-" {
			label = "anomaly observed"
		}
		entries = append(entries, policyTimelineEntry{
			When:  row.Timestamp,
			Label: label,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].When < entries[j].When
	})
	return entries
}

func buildAnomalyMeaning(resp anomalyListResponse) []string {
	out := make([]string, 0, 3)
	if resp.Count == 1 {
		out = append(out, "This query returned a single anomaly.")
	} else {
		out = append(out, fmt.Sprintf("This query returned %d anomalies.", resp.Count))
	}
	highest := highestSeverity(resp.Anomalies)
	if highest != "" {
		out = append(out, "The highest observed severity in this result set is "+renderStatus(highest)+".")
	}
	if latest := latestAnomaly(resp.Anomalies); latest != nil && latest.Confidence > 0 {
		out = append(out, fmt.Sprintf("The newest anomaly has confidence %.2f.", latest.Confidence))
	}
	return out
}

func buildAnomalyRecentEntries(rows []struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	BlockHeight uint64  `json:"block_height"`
	Timestamp   int64   `json:"timestamp"`
	Confidence  float64 `json:"confidence"`
	TxHash      string  `json:"tx_hash"`
}) []anomalyRecentEntry {
	sorted := append([]struct {
		ID          string  `json:"id"`
		Type        string  `json:"type"`
		Severity    string  `json:"severity"`
		Title       string  `json:"title"`
		Description string  `json:"description"`
		Source      string  `json:"source"`
		BlockHeight uint64  `json:"block_height"`
		Timestamp   int64   `json:"timestamp"`
		Confidence  float64 `json:"confidence"`
		TxHash      string  `json:"tx_hash"`
	}{}, rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].Timestamp == sorted[j].Timestamp {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].Timestamp > sorted[j].Timestamp
	})
	recent := make([]anomalyRecentEntry, 0, len(sorted))
	for _, row := range sorted {
		recent = append(recent, anomalyRecentEntry{
			ID:         blankDash(row.ID),
			Severity:   blankDash(renderStatus(row.Severity)),
			Type:       blankDash(row.Type),
			Source:     blankDash(row.Source),
			Confidence: fmt.Sprintf("%.2f", row.Confidence),
			Block:      fmt.Sprintf("%d", row.BlockHeight),
		})
	}
	return recent
}

func anomalySuggestions(resp anomalyListResponse) []string {
	suggestions := []string{"kubectl cybermesh anomalies list --limit 20"}
	if latest := latestAnomaly(resp.Anomalies); latest != nil {
		if strings.TrimSpace(latest.ID) != "" {
			suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh trace anomaly %s", latest.ID))
		}
	}
	return suggestions
}

func countDistinctAnomalies(rows []struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	BlockHeight uint64  `json:"block_height"`
	Timestamp   int64   `json:"timestamp"`
	Confidence  float64 `json:"confidence"`
	TxHash      string  `json:"tx_hash"`
}, pick func(struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	BlockHeight uint64  `json:"block_height"`
	Timestamp   int64   `json:"timestamp"`
	Confidence  float64 `json:"confidence"`
	TxHash      string  `json:"tx_hash"`
}) string) int {
	seen := map[string]struct{}{}
	for _, row := range rows {
		value := strings.TrimSpace(pick(row))
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	return len(seen)
}

func latestAnomalyTimestamp(rows []struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	BlockHeight uint64  `json:"block_height"`
	Timestamp   int64   `json:"timestamp"`
	Confidence  float64 `json:"confidence"`
	TxHash      string  `json:"tx_hash"`
}) int64 {
	var latest int64
	for _, row := range rows {
		if row.Timestamp > latest {
			latest = row.Timestamp
		}
	}
	return latest
}

func latestAnomaly(rows []struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	BlockHeight uint64  `json:"block_height"`
	Timestamp   int64   `json:"timestamp"`
	Confidence  float64 `json:"confidence"`
	TxHash      string  `json:"tx_hash"`
}) *struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	BlockHeight uint64  `json:"block_height"`
	Timestamp   int64   `json:"timestamp"`
	Confidence  float64 `json:"confidence"`
	TxHash      string  `json:"tx_hash"`
} {
	if len(rows) == 0 {
		return nil
	}
	latest := &rows[0]
	for i := 1; i < len(rows); i++ {
		if rows[i].Timestamp > latest.Timestamp {
			latest = &rows[i]
		}
	}
	return latest
}

func highestSeverity(rows []struct {
	ID          string  `json:"id"`
	Type        string  `json:"type"`
	Severity    string  `json:"severity"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Source      string  `json:"source"`
	BlockHeight uint64  `json:"block_height"`
	Timestamp   int64   `json:"timestamp"`
	Confidence  float64 `json:"confidence"`
	TxHash      string  `json:"tx_hash"`
}) string {
	rank := map[string]int{"low": 1, "medium": 2, "high": 3, "critical": 4}
	best := ""
	bestRank := 0
	for _, row := range rows {
		value := strings.ToLower(strings.TrimSpace(row.Severity))
		if rank[value] > bestRank {
			best = value
			bestRank = rank[value]
		}
	}
	return best
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
	renderPolicySummary(r.out, buildPolicyDetailView(resp))
	renderSuggestions(r.out, policySuggestions(resp))
	return nil
}

type policyDetailView struct {
	SummaryFields []detailField
	Timeline      []policyTimelineEntry
	LatestAck     []detailField
	Meaning       []string
}

type policyTimelineEntry struct {
	When  int64
	Label string
}

func buildPolicyDetailView(resp policyDetailResult) policyDetailView {
	view := policyDetailView{
		SummaryFields: []detailField{
			{Key: "ID", Value: blankDash(resp.Summary.PolicyID)},
			{Key: "Status", Value: policyDisplayStatus(resp)},
			{Key: "Workflow", Value: blankDash(resp.Summary.WorkflowID)},
			{Key: "Trace", Value: blankDash(resp.Summary.TraceID)},
			{Key: "Request", Value: blankDash(resp.Summary.RequestID)},
			{Key: "Command", Value: blankDash(resp.Summary.CommandID)},
			{Key: "Latest Outbox", Value: blankDash(resp.Summary.LatestOutboxID)},
			{Key: "Source", Value: blankDash(joinParts(resp.Summary.SourceType, resp.Summary.SourceID))},
			{Key: "Scope", Value: blankDash(resp.Summary.ScopeIdentifier)},
			{Key: "Updated At", Value: formatTimestampAuto(resp.Summary.LatestCreatedAt)},
		},
		Timeline:  buildPolicyTimeline(resp),
		LatestAck: buildPolicyLatestAck(resp),
		Meaning:   buildPolicyMeaning(resp),
	}
	return view
}

func renderPolicySummary(w io.Writer, view policyDetailView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			_, _ = fmt.Fprintf(w, "- %s  %s\n", formatTimestampAuto(item.When), item.Label)
		}
	}
	if len(view.LatestAck) > 0 {
		renderSubTitle(w, "Latest ACK")
		renderFields(w, view.LatestAck)
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, line := range view.Meaning {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", line)
		}
	}
}

func buildPolicyTimeline(resp policyDetailResult) []policyTimelineEntry {
	entries := make([]policyTimelineEntry, 0, 5)
	firstOutboxAt := int64(0)
	if len(resp.Trace.Outbox) > 0 {
		firstOutboxAt = resp.Trace.Outbox[0].CreatedAt
		for _, row := range resp.Trace.Outbox[1:] {
			if row.CreatedAt > 0 && (firstOutboxAt == 0 || row.CreatedAt < firstOutboxAt) {
				firstOutboxAt = row.CreatedAt
			}
		}
	}
	if firstOutboxAt > 0 {
		entries = append(entries, policyTimelineEntry{When: firstOutboxAt, Label: "Policy recorded"})
	}
	if resp.Summary.LatestCreatedAt > 0 && resp.Summary.LatestCreatedAt != firstOutboxAt {
		entries = append(entries, policyTimelineEntry{When: resp.Summary.LatestCreatedAt, Label: "Latest policy state recorded"})
	}
	if resp.Summary.LatestPublishedAt > 0 {
		entries = append(entries, policyTimelineEntry{When: resp.Summary.LatestPublishedAt, Label: "Latest delivery published"})
	}
	if latestAck := latestAckRow(resp); latestAck != nil {
		label := "Latest ACK recorded"
		if result := strings.ToLower(strings.TrimSpace(latestAck.Result)); result != "" {
			label = "Latest ACK " + result
		}
		if code := strings.TrimSpace(latestAck.ErrorCode); code != "" {
			label += ": " + code
		}
		entries = append(entries, policyTimelineEntry{When: policyAckWhen(*latestAck), Label: label})
	} else if resp.Summary.LatestAckedAt > 0 {
		label := "Latest ACK recorded"
		if result := strings.ToLower(strings.TrimSpace(resp.Summary.LatestAckResult)); result != "" {
			label = "Latest ACK " + result
		}
		entries = append(entries, policyTimelineEntry{When: resp.Summary.LatestAckedAt, Label: label})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].When < entries[j].When
	})
	return entries
}

func buildPolicyLatestAck(resp policyDetailResult) []detailField {
	ack := latestAckRow(resp)
	if ack == nil && strings.TrimSpace(resp.Summary.LatestAckResult) == "" && strings.TrimSpace(resp.Summary.LatestAckEventID) == "" {
		return []detailField{
			{Key: "Result", Value: "PENDING"},
			{Key: "Reason", Value: "No ACK recorded yet"},
		}
	}
	fields := make([]detailField, 0, 5)
	if ack != nil {
		fields = append(fields, detailField{Key: "Result", Value: blankDash(renderStatus(ack.Result))})
		if reason := strings.TrimSpace(ack.Reason); reason != "" {
			fields = append(fields, detailField{Key: "Reason", Value: reason})
		}
		if code := strings.TrimSpace(ack.ErrorCode); code != "" {
			fields = append(fields, detailField{Key: "Error", Value: code})
		}
		if controller := strings.TrimSpace(ack.ControllerInstance); controller != "" {
			fields = append(fields, detailField{Key: "Controller", Value: controller})
		}
		if when := policyAckWhen(*ack); when > 0 {
			fields = append(fields, detailField{Key: "Observed At", Value: formatTimestampAuto(when)})
		}
		if eventID := strings.TrimSpace(ack.AckEventID); eventID != "" {
			fields = append(fields, detailField{Key: "Event", Value: eventID})
		}
		return fields
	}
	fields = append(fields, detailField{Key: "Result", Value: blankDash(renderStatus(resp.Summary.LatestAckResult))})
	if controller := strings.TrimSpace(resp.Summary.LatestAckController); controller != "" {
		fields = append(fields, detailField{Key: "Controller", Value: controller})
	}
	if when := resp.Summary.LatestAckedAt; when > 0 {
		fields = append(fields, detailField{Key: "Observed At", Value: formatTimestampAuto(when)})
	}
	if eventID := strings.TrimSpace(resp.Summary.LatestAckEventID); eventID != "" {
		fields = append(fields, detailField{Key: "Event", Value: eventID})
	}
	return fields
}

func buildPolicyMeaning(resp policyDetailResult) []string {
	out := make([]string, 0, 3)
	switch strings.ToLower(strings.TrimSpace(resp.Summary.LatestStatus)) {
	case "acked":
		out = append(out, "The latest outbox row reached an ACKed state.")
	case "published":
		out = append(out, "The latest outbox row was published but final ACK state is not yet recorded.")
	case "pending", "publishing", "retry":
		out = append(out, fmt.Sprintf("The policy is still in %s and may not be fully applied yet.", strings.ToLower(strings.TrimSpace(resp.Summary.LatestStatus))))
	case "terminal_failed":
		out = append(out, "The latest outbox row reached a terminal failure state.")
	}
	if ack := latestAckRow(resp); ack != nil {
		switch strings.ToLower(strings.TrimSpace(ack.Result)) {
		case "applied":
			out = append(out, "The downstream controller reported that the latest policy state was applied.")
		case "failed":
			if reason := strings.TrimSpace(ack.Reason); reason != "" {
				out = append(out, "The downstream controller reported a failure: "+reason+".")
			} else {
				out = append(out, "The downstream controller reported a failure for the latest policy state.")
			}
		case "rejected":
			out = append(out, "The downstream controller rejected the latest policy state.")
		}
	} else if resp.Summary.AckCount == 0 {
		out = append(out, "No ACK history is recorded for this policy yet.")
	}
	if len(out) == 0 {
		out = append(out, "The policy summary is available, but there is not enough delivery history to infer more yet.")
	}
	return out
}

func latestAckRow(resp policyDetailResult) *ackRow {
	var best *ackRow
	for i := range resp.Trace.Acks {
		row := &resp.Trace.Acks[i]
		if best == nil || policyAckWhen(*row) > policyAckWhen(*best) {
			best = row
		}
	}
	return best
}

func policyAckWhen(row ackRow) int64 {
	if row.ObservedAt > 0 {
		return row.ObservedAt
	}
	return 0
}

func policyDisplayStatus(resp policyDetailResult) string {
	status := strings.TrimSpace(resp.Summary.LatestStatus)
	if ack := latestAckRow(resp); ack != nil && strings.EqualFold(strings.TrimSpace(ack.Result), "failed") {
		return "DECISION FAILED"
	}
	return renderStatus(status)
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
	renderTitle(r.out, "ACK")
	renderAckSummary(r.out, buildAckListView(resp))
	renderSuggestions(r.out, ackSuggestions(resp))
	return nil
}

type ackListView struct {
	SummaryFields []detailField
	Timeline      []policyTimelineEntry
	LatestAck     []detailField
	Meaning       []string
	Recent        []ackRow
}

func buildAckListView(resp ackListResponse) ackListView {
	view := ackListView{
		SummaryFields: []detailField{
			{Key: "Matches", Value: fmt.Sprintf("%d", len(resp.Rows))},
			{Key: "Policies", Value: fmt.Sprintf("%d", countDistinctAckField(resp.Rows, func(row ackRow) string { return row.PolicyID }))},
			{Key: "Workflows", Value: fmt.Sprintf("%d", countDistinctAckField(resp.Rows, func(row ackRow) string { return row.WorkflowID }))},
			{Key: "Controllers", Value: fmt.Sprintf("%d", countDistinctAckField(resp.Rows, func(row ackRow) string { return row.ControllerInstance }))},
			{Key: "Latest Observed", Value: formatTimestampAuto(latestAckObserved(resp.Rows))},
		},
		Timeline: buildAckTimeline(resp.Rows),
		LatestAck: buildLatestAckFields(resp.Rows),
		Meaning:  buildAckMeaning(resp.Rows),
		Recent:   sortedAckRows(resp.Rows),
	}
	return view
}

func renderAckSummary(w io.Writer, view ackListView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			_, _ = fmt.Fprintf(w, "- %s  %s\n", formatTimestampAuto(item.When), item.Label)
		}
	}
	if len(view.LatestAck) > 0 {
		renderSubTitle(w, "Latest ACK")
		renderFields(w, view.LatestAck)
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, line := range view.Meaning {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", line)
		}
	}
	if len(view.Recent) > 0 {
		renderSubTitle(w, "Recent ACKs")
		for i, row := range view.Recent {
			if i >= 5 {
				break
			}
			_, _ = fmt.Fprintln(w, detailLine("Policy", row.PolicyID))
			_, _ = fmt.Fprintln(w, detailLine("Result", renderStatus(row.Result)))
			_, _ = fmt.Fprintln(w, detailLine("Controller", row.ControllerInstance))
			_, _ = fmt.Fprintln(w, detailLine("Scope", row.ScopeIdentifier))
			_, _ = fmt.Fprintln(w, detailLine("ACK Event", row.AckEventID))
			_, _ = fmt.Fprintln(w, detailLine("Workflow", row.WorkflowID))
			_, _ = fmt.Fprintln(w, detailLine("When", formatTimestampAuto(row.ObservedAt)))
			_, _ = fmt.Fprintln(w)
		}
	}
}

func buildAckTimeline(rows []ackRow) []policyTimelineEntry {
	items := sortedAckRows(rows)
	out := make([]policyTimelineEntry, 0, len(items))
	for _, row := range items {
		label := joinParts(strings.ToLower(strings.TrimSpace(row.Result)), strings.TrimSpace(row.ControllerInstance), strings.TrimSpace(row.ScopeIdentifier))
		if strings.TrimSpace(row.ErrorCode) != "" {
			label = joinParts(label, strings.TrimSpace(row.ErrorCode))
		}
		if label == "-" {
			label = "ack observed"
		}
		out = append(out, policyTimelineEntry{When: row.ObservedAt, Label: label})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].When == out[j].When {
			return out[i].Label < out[j].Label
		}
		return out[i].When < out[j].When
	})
	return out
}

func buildLatestAckFields(rows []ackRow) []detailField {
	latest := latestAckListRow(rows)
	if latest == nil {
		return nil
	}
	fields := []detailField{
		{Key: "Policy", Value: latest.PolicyID},
		{Key: "Result", Value: renderStatus(latest.Result)},
		{Key: "Controller", Value: latest.ControllerInstance},
		{Key: "Scope", Value: latest.ScopeIdentifier},
		{Key: "Observed At", Value: formatTimestampAuto(latest.ObservedAt)},
		{Key: "ACK Event", Value: latest.AckEventID},
	}
	if strings.TrimSpace(latest.Reason) != "" {
		fields = append(fields, detailField{Key: "Reason", Value: latest.Reason})
	}
	if strings.TrimSpace(latest.ErrorCode) != "" {
		fields = append(fields, detailField{Key: "Error", Value: latest.ErrorCode})
	}
	if strings.TrimSpace(latest.WorkflowID) != "" {
		fields = append(fields, detailField{Key: "Workflow", Value: latest.WorkflowID})
	}
	return fields
}

func buildAckMeaning(rows []ackRow) []string {
	out := make([]string, 0, 4)
	if len(rows) == 1 {
		out = append(out, "This query returned a single ACK row.")
	} else {
		out = append(out, fmt.Sprintf("This query returned %d ACK rows.", len(rows)))
	}
	if latest := latestAckListRow(rows); latest != nil {
		switch strings.ToLower(strings.TrimSpace(latest.Result)) {
		case "failed":
			if reason := strings.TrimSpace(latest.Reason); reason != "" {
				out = append(out, "The latest ACK reports a failure: "+reason+".")
			} else {
				out = append(out, "The latest ACK reports a failure state.")
			}
		case "applied", "acked":
			out = append(out, "The latest ACK indicates the downstream controller applied the current state.")
		}
	}
	if count := countDistinctAckField(rows, func(row ackRow) string { return row.WorkflowID }); count > 0 {
		out = append(out, fmt.Sprintf("The results span %d workflow contexts.", count))
	}
	return out
}

func ackSuggestions(resp ackListResponse) []string {
	suggestions := []string{"kubectl cybermesh monitor"}
	if latest := latestAckListRow(resp.Rows); latest != nil {
		if strings.TrimSpace(latest.PolicyID) != "" {
			suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh policies get %s", latest.PolicyID))
		}
		if strings.TrimSpace(latest.WorkflowID) != "" {
			suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh workflows get %s", latest.WorkflowID))
			suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh audit get --workflow-id %s --limit 10", latest.WorkflowID))
		}
		if strings.TrimSpace(latest.TraceID) != "" {
			suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh trace trace %s", latest.TraceID))
		}
	}
	return suggestions
}

func latestAckListRow(rows []ackRow) *ackRow {
	var best *ackRow
	for i := range rows {
		row := &rows[i]
		if best == nil || row.ObservedAt > best.ObservedAt {
			best = row
		}
	}
	return best
}

func latestAckObserved(rows []ackRow) int64 {
	if latest := latestAckListRow(rows); latest != nil {
		return latest.ObservedAt
	}
	return 0
}

func sortedAckRows(rows []ackRow) []ackRow {
	sorted := append([]ackRow{}, rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].ObservedAt == sorted[j].ObservedAt {
			return sorted[i].AckEventID < sorted[j].AckEventID
		}
		return sorted[i].ObservedAt > sorted[j].ObservedAt
	})
	return sorted
}

func countDistinctAckField(rows []ackRow, pick func(ackRow) string) int {
	seen := map[string]struct{}{}
	for _, row := range rows {
		value := strings.TrimSpace(pick(row))
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	return len(seen)
}

func (r *commandRunner) renderValidators(resp validatorListResponse) error {
	renderTitle(r.out, "Validators")
	renderValidatorsSummary(r.out, buildValidatorsView(resp))
	renderSuggestions(r.out, validatorSuggestions(resp))
	return nil
}

type validatorsView struct {
	SummaryFields []detailField
	Timeline      []string
	Meaning       []string
	Snapshot      []validatorSnapshotRow
}

type validatorSnapshotRow struct {
	ID        string
	Status    string
	PublicKey string
}

func buildValidatorsView(resp validatorListResponse) validatorsView {
	snapshot := make([]validatorSnapshotRow, 0, len(resp.Validators))
	for _, item := range resp.Validators {
		snapshot = append(snapshot, validatorSnapshotRow{
			ID:        item.ID,
			Status:    item.Status,
			PublicKey: item.PublicKey,
		})
	}
	if len(snapshot) > 5 {
		snapshot = snapshot[:5]
	}
	return validatorsView{
		SummaryFields: []detailField{
			{Key: "Total", Value: fmt.Sprintf("%d", resp.Total)},
			{Key: "Active", Value: fmt.Sprintf("%d", countValidatorsByStatus(resp.Validators, "active"))},
			{Key: "Inactive", Value: fmt.Sprintf("%d", countValidatorsByStatus(resp.Validators, "inactive"))},
			{Key: "Other Statuses", Value: fmt.Sprintf("%d", countOtherValidatorStatuses(resp.Validators))},
		},
		Timeline: buildValidatorsTimeline(resp),
		Meaning:  buildValidatorsMeaning(resp),
		Snapshot: snapshot,
	}
}

func renderValidatorsSummary(w io.Writer, view validatorsView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, line := range view.Meaning {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", line)
		}
	}
	if len(view.Snapshot) > 0 {
		renderSubTitle(w, "Validator Snapshot")
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "VALIDATOR_ID\tSTATUS\tPUBLIC_KEY")
		for _, item := range view.Snapshot {
			fmt.Fprintf(tw, "%s\t%s\t%s\n",
				shortID(item.ID, 22),
				renderStatus(item.Status),
				shortID(item.PublicKey, 24),
			)
		}
		_ = tw.Flush()
	}
}

func buildValidatorsTimeline(resp validatorListResponse) []string {
	lines := make([]string, 0, 3)
	active := countValidatorsByStatus(resp.Validators, "active")
	inactive := countValidatorsByStatus(resp.Validators, "inactive")
	if resp.Total == 0 {
		return []string{"No validators were returned by the backend."}
	}
	lines = append(lines, fmt.Sprintf("%d validators are currently reported by the backend.", resp.Total))
	lines = append(lines, fmt.Sprintf("%d validators are currently active.", active))
	if inactive > 0 {
		lines = append(lines, fmt.Sprintf("%d validators are currently inactive.", inactive))
	}
	return lines
}

func buildValidatorsMeaning(resp validatorListResponse) []string {
	active := countValidatorsByStatus(resp.Validators, "active")
	inactive := countValidatorsByStatus(resp.Validators, "inactive")
	out := make([]string, 0, 3)
	switch {
	case resp.Total == 0:
		out = append(out, "No validator rows were returned, so validator registration or backend reachability should be checked.")
	case inactive == 0 && active == resp.Total:
		out = append(out, "All reported validators are currently active.")
	case inactive > 0:
		out = append(out, fmt.Sprintf("%d validators are inactive and should be investigated.", inactive))
	default:
		out = append(out, "Validator status includes mixed or non-standard values.")
	}
	if active > 0 {
		out = append(out, fmt.Sprintf("The backend currently reports %d active validators.", active))
	}
	return out
}

func validatorSuggestions(resp validatorListResponse) []string {
	suggestions := []string{"kubectl cybermesh consensus"}
	if countValidatorsByStatus(resp.Validators, "inactive") > 0 {
		suggestions = append(suggestions, "kubectl cybermesh validators --status inactive")
	}
	suggestions = append(suggestions, "kubectl cybermesh doctor")
	return suggestions
}

func countValidatorsByStatus(rows []struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
	Status    string `json:"status"`
}, status string) int {
	count := 0
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.Status), status) {
			count++
		}
	}
	return count
}

func countOtherValidatorStatuses(rows []struct {
	ID        string `json:"id"`
	PublicKey string `json:"public_key"`
	Status    string `json:"status"`
}) int {
	count := 0
	for _, row := range rows {
		status := strings.ToLower(strings.TrimSpace(row.Status))
		if status == "" || status == "active" || status == "inactive" {
			continue
		}
		count++
	}
	return count
}

func (r *commandRunner) renderConsensus(resp consensusOverview) error {
	renderTitle(r.out, "Consensus")
	renderConsensusSummary(r.out, buildConsensusView(resp))
	renderSuggestions(r.out, consensusSuggestions(resp))
	return nil
}

type consensusView struct {
	SummaryFields    []detailField
	Timeline         []string
	Meaning          []string
	SuspiciousFields []detailField
}

func buildConsensusView(resp consensusOverview) consensusView {
	return consensusView{
		SummaryFields: []detailField{
			{Key: "Leader", Value: blankDash(shortID(resp.Leader, 20))},
			{Key: "Leader ID", Value: blankDash(shortID(resp.LeaderID, 20))},
			{Key: "Term", Value: fmt.Sprintf("%d", resp.Term)},
			{Key: "Phase", Value: renderStatus(resp.Phase)},
			{Key: "Active Peers", Value: fmt.Sprintf("%d", resp.ActivePeers)},
			{Key: "Quorum Size", Value: fmt.Sprintf("%d", resp.QuorumSize)},
			{Key: "Updated At", Value: blankDash(resp.UpdatedAt)},
		},
		Timeline:         buildConsensusTimeline(resp),
		Meaning:          buildConsensusMeaning(resp),
		SuspiciousFields: buildConsensusSuspiciousFields(resp),
	}
}

func renderConsensusSummary(w io.Writer, view consensusView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, item := range view.Meaning {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.SuspiciousFields) > 0 {
		renderSubTitle(w, "Suspicious Nodes")
		renderFields(w, view.SuspiciousFields)
	}
}

func buildConsensusTimeline(resp consensusOverview) []string {
	items := make([]string, 0, len(resp.Proposals)+len(resp.Votes)+2)
	if resp.Leader != "" || resp.LeaderID != "" {
		items = append(items, fmt.Sprintf(
			"Leader %s is active in phase %s",
			blankDash(shortID(firstNonEmpty(resp.Leader, resp.LeaderID), 20)),
			renderStatus(resp.Phase),
		))
	}
	if resp.QuorumSize > 0 {
		items = append(items, fmt.Sprintf("Quorum target is %d peers; %d active peers are reporting", resp.QuorumSize, resp.ActivePeers))
	}
	for _, p := range resp.Proposals {
		items = append(items, fmt.Sprintf(
			"%s proposal for block %d view %d by %s",
			formatTimestampAuto(p.Timestamp),
			p.Block,
			p.View,
			blankDash(shortID(p.Proposer, 20)),
		))
	}
	for _, v := range resp.Votes {
		items = append(items, fmt.Sprintf(
			"%s vote activity: %s count %d",
			formatTimestampAuto(v.Timestamp),
			renderStatus(v.Type),
			v.Count,
		))
	}
	return compactStringList(items, 10)
}

func buildConsensusMeaning(resp consensusOverview) []string {
	items := make([]string, 0, 3)
	if resp.QuorumSize == 0 {
		items = append(items, "Quorum size is not reported, so consensus health is only partially visible.")
	} else if resp.ActivePeers >= resp.QuorumSize {
		items = append(items, "Consensus currently has enough active peers to satisfy quorum.")
	} else {
		items = append(items, "Active peers are below quorum, so consensus may be degraded or stalled.")
	}
	if len(resp.SuspiciousNodes) > 0 {
		items = append(items, fmt.Sprintf("%d suspicious node entries need investigation before they affect consensus stability.", len(resp.SuspiciousNodes)))
	}
	if len(resp.Proposals) == 0 && len(resp.Votes) == 0 {
		items = append(items, "No recent proposal or vote activity is visible in this snapshot.")
	}
	return items
}

func buildConsensusSuspiciousFields(resp consensusOverview) []detailField {
	if len(resp.SuspiciousNodes) == 0 {
		return nil
	}
	fields := make([]detailField, 0, minInt(len(resp.SuspiciousNodes), 3)+1)
	for i, node := range resp.SuspiciousNodes {
		if i >= 3 {
			fields = append(fields, detailField{Key: "Additional Nodes", Value: fmt.Sprintf("%d more", len(resp.SuspiciousNodes)-i)})
			break
		}
		fields = append(fields, detailField{
			Key: fmt.Sprintf("Node %d", i+1),
			Value: fmt.Sprintf("%s | status=%s | uptime=%.2f | score=%.2f | reason=%s",
				shortID(node.ID, 20),
				renderStatus(node.Status),
				node.Uptime,
				node.SuspicionScore,
				blankDash(node.Reason),
			),
		})
	}
	return fields
}

func consensusSuggestions(resp consensusOverview) []string {
	suggestions := []string{"kubectl cybermesh validators", "kubectl cybermesh control", "kubectl cybermesh leases"}
	if len(resp.SuspiciousNodes) > 0 {
		suggestions = append([]string{"kubectl cybermesh ai suspicious-nodes"}, suggestions...)
	}
	return suggestions
}

func (r *commandRunner) renderBacklog(resp outboxBacklogResult) error {
	renderTitle(r.out, "Backlog")
	renderBacklogSummary(r.out, buildBacklogView(resp))
	renderSuggestions(r.out, backlogSuggestions(resp))
	return nil
}

type backlogView struct {
	SummaryFields []detailField
	Timeline      []string
	Meaning       []string
}

func buildBacklogView(resp outboxBacklogResult) backlogView {
	view := backlogView{
		SummaryFields: []detailField{
			{Key: "Pending", Value: fmt.Sprintf("%d", resp.Pending)},
			{Key: "Retry", Value: fmt.Sprintf("%d", resp.Retry)},
			{Key: "Publishing", Value: fmt.Sprintf("%d", resp.Publishing)},
			{Key: "Published Rows", Value: fmt.Sprintf("%d", resp.PublishedRows)},
			{Key: "Acked Rows", Value: fmt.Sprintf("%d", resp.AckedRows)},
			{Key: "Terminal Rows", Value: fmt.Sprintf("%d", resp.TerminalRows)},
			{Key: "Total Rows", Value: fmt.Sprintf("%d", resp.TotalRows)},
			{Key: "Oldest Pending Age", Value: formatDurationMillis(resp.OldestPendingAgeMs)},
			{Key: "ACK Closure Ratio", Value: fmt.Sprintf("%.2f", resp.AckClosureRatio)},
		},
		Timeline: buildBacklogTimeline(resp),
		Meaning:  buildBacklogMeaning(resp),
	}
	return view
}

func renderBacklogSummary(w io.Writer, view backlogView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, line := range view.Meaning {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", line)
		}
	}
}

func buildBacklogTimeline(resp outboxBacklogResult) []string {
	lines := make([]string, 0, 4)
	if resp.OldestPendingAgeMs > 0 {
		lines = append(lines, "Oldest pending row has been waiting "+formatDurationMillis(resp.OldestPendingAgeMs)+".")
	}
	if resp.Pending > 0 {
		lines = append(lines, fmt.Sprintf("%d rows are currently pending dispatch.", resp.Pending))
	} else {
		lines = append(lines, "No rows are currently pending dispatch.")
	}
	if resp.Retry > 0 {
		lines = append(lines, fmt.Sprintf("%d rows are retrying after earlier delivery failures.", resp.Retry))
	}
	if resp.Publishing > 0 {
		lines = append(lines, fmt.Sprintf("%d rows are currently in the publishing stage.", resp.Publishing))
	}
	return lines
}

func buildBacklogMeaning(resp outboxBacklogResult) []string {
	out := make([]string, 0, 4)
	switch {
	case resp.Pending == 0 && resp.Retry == 0 && resp.Publishing == 0:
		out = append(out, "The outbox is currently clear and no backlog pressure is visible.")
	case resp.Pending > 0 && resp.Retry == 0:
		out = append(out, fmt.Sprintf("There are %d rows waiting to be dispatched, but no retry pressure is currently visible.", resp.Pending))
	case resp.Retry > 0:
		out = append(out, fmt.Sprintf("There are %d rows retrying, which indicates recent delivery failures or downstream friction.", resp.Retry))
	}
	if resp.AckClosureRatio >= 1 {
		out = append(out, "ACK closure is currently keeping pace with published rows.")
	} else {
		out = append(out, "ACK closure is lagging behind published rows and should be monitored.")
	}
	if resp.TerminalRows > 0 {
		out = append(out, fmt.Sprintf("%d rows have reached terminal states in the current retained backlog view.", resp.TerminalRows))
	}
	return out
}

func backlogSuggestions(resp outboxBacklogResult) []string {
	suggestions := []string{
		"kubectl cybermesh control",
		"kubectl cybermesh leases",
	}
	if resp.Retry > 0 || resp.Pending > 0 || resp.Publishing > 0 {
		suggestions = append(suggestions, "kubectl cybermesh monitor")
	}
	return suggestions
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
	renderSafeModeSummary(r.out, buildSafeModeView(resp))
	renderSuggestions(r.out, safeModeSuggestions(resp))
	return nil
}

type safeModeView struct {
	SummaryFields []detailField
	Timeline      []string
	Meaning       []string
}

func buildSafeModeView(resp safeModeToggleResult) safeModeView {
	state := "disabled"
	if resp.Enabled {
		state = "enabled"
	}
	return safeModeView{
		SummaryFields: []detailField{
			{Key: "State", Value: renderStatus(state)},
			{Key: "Mutation Gate", Value: map[bool]string{true: "restricted", false: "normal"}[resp.Enabled]},
		},
		Timeline: buildSafeModeTimeline(resp),
		Meaning:  buildSafeModeMeaning(resp),
	}
}

func renderSafeModeSummary(w io.Writer, view safeModeView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, item := range view.Meaning {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
}

func buildSafeModeTimeline(resp safeModeToggleResult) []string {
	if resp.Enabled {
		return []string{
			"Safe mode has been enabled for the control plane.",
			"High-risk mutation paths should now be treated as operator-review flows.",
		}
	}
	return []string{
		"Safe mode has been disabled for the control plane.",
		"Normal mutation paths can proceed unless another global gate is active.",
	}
}

func buildSafeModeMeaning(resp safeModeToggleResult) []string {
	if resp.Enabled {
		return []string{
			"Safe mode narrows mutation behavior so operators can stabilize the control plane before retrying risky actions.",
			"Follow up with control or backlog views to verify the cluster is behaving as expected.",
		}
	}
	return []string{
		"Safe mode is no longer restricting normal control mutations.",
		"Verify control state and backlog before resuming broader operator activity.",
	}
}

func safeModeSuggestions(resp safeModeToggleResult) []string {
	if resp.Enabled {
		return []string{
			"kubectl cybermesh control",
			"kubectl cybermesh backlog",
			"kubectl cybermesh safe-mode disable --reason-code <code> --reason-text <text> --yes",
		}
	}
	return []string{
		"kubectl cybermesh control",
		"kubectl cybermesh backlog",
	}
}

func (r *commandRunner) renderKillSwitchResult(resp killSwitchToggleResult) error {
	renderTitle(r.out, "Kill Switch")
	renderKillSwitchSummary(r.out, buildKillSwitchView(resp))
	renderSuggestions(r.out, killSwitchSuggestions(resp))
	return nil
}

type killSwitchView struct {
	SummaryFields []detailField
	Timeline      []string
	Meaning       []string
}

func buildKillSwitchView(resp killSwitchToggleResult) killSwitchView {
	state := "disabled"
	if resp.Enabled {
		state = "enabled"
	}
	return killSwitchView{
		SummaryFields: []detailField{
			{Key: "State", Value: renderStatus(state)},
			{Key: "Mutation Gate", Value: map[bool]string{true: "blocked", false: "normal"}[resp.Enabled]},
		},
		Timeline: buildKillSwitchTimeline(resp),
		Meaning:  buildKillSwitchMeaning(resp),
	}
}

func renderKillSwitchSummary(w io.Writer, view killSwitchView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, item := range view.Meaning {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
}

func buildKillSwitchTimeline(resp killSwitchToggleResult) []string {
	if resp.Enabled {
		return []string{
			"Kill switch has been enabled for the control plane.",
			"Write-side control mutations should now be treated as blocked until the switch is disabled.",
		}
	}
	return []string{
		"Kill switch has been disabled for the control plane.",
		"Write-side control mutations can proceed unless another global gate is active.",
	}
}

func buildKillSwitchMeaning(resp killSwitchToggleResult) []string {
	if resp.Enabled {
		return []string{
			"The kill switch is the strongest global mutation gate and should only remain enabled while the control plane is being protected or stabilized.",
			"Verify control state before re-enabling any write-side operator workflows.",
		}
	}
	return []string{
		"The kill switch is no longer blocking control mutations.",
		"Confirm control state and backlog before resuming normal operator actions.",
	}
}

func killSwitchSuggestions(resp killSwitchToggleResult) []string {
	if resp.Enabled {
		return []string{
			"kubectl cybermesh control",
			"kubectl cybermesh backlog",
			"kubectl cybermesh kill-switch disable --reason-code <code> --reason-text <text> --yes",
		}
	}
	return []string{
		"kubectl cybermesh control",
		"kubectl cybermesh backlog",
	}
}

func (r *commandRunner) renderDoctor(resp doctorResult) error {
	renderTitle(r.out, "Doctor")
	renderDoctorSummary(r.out, buildDoctorView(resp))
	renderSuggestions(r.out, doctorSuggestions(resp))
	return nil
}

func (r *commandRunner) renderVersionVerbose(info map[string]any) error {
	renderTitle(r.out, "Version")
	renderVersionSummary(r.out, buildVersionView(info))
	renderSuggestions(r.out, versionSuggestions(info))
	return nil
}

type versionView struct {
	SummaryFields []detailField
	Timeline      []string
	Meaning       []string
}

func buildVersionView(info map[string]any) versionView {
	return versionView{
		SummaryFields: []detailField{
			{Key: "Version", Value: blankDash(stringValue(info["version"]))},
			{Key: "Commit", Value: blankDash(stringValue(info["commit"]))},
			{Key: "Build Time", Value: blankDash(stringValue(info["build_time"]))},
			{Key: "Base URL", Value: blankDash(stringValue(info["base_url"]))},
			{Key: "Output", Value: blankDash(stringValue(info["output"]))},
			{Key: "Interactive Mode", Value: blankDash(stringValue(info["interactive_mode"]))},
			{Key: "Tenant", Value: blankDash(stringValue(info["tenant"]))},
			{Key: "Auth Mode", Value: blankDash(stringValue(info["auth_mode"]))},
		},
		Timeline: buildVersionTimeline(info),
		Meaning:  buildVersionMeaning(info),
	}
}

func renderVersionSummary(w io.Writer, view versionView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, item := range view.Meaning {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
}

func buildVersionTimeline(info map[string]any) []string {
	lines := []string{
		fmt.Sprintf("CLI version %s is running against %s.", blankDash(stringValue(info["version"])), blankDash(stringValue(info["base_url"]))),
		fmt.Sprintf("The current build was produced from commit %s at %s.", blankDash(stringValue(info["commit"])), blankDash(stringValue(info["build_time"]))),
		fmt.Sprintf("Output mode is %s and interactive mode is %s.", blankDash(stringValue(info["output"])), blankDash(stringValue(info["interactive_mode"]))),
	}
	if tenant := strings.TrimSpace(stringValue(info["tenant"])); tenant != "" {
		lines = append(lines, fmt.Sprintf("Tenant context is %s.", tenant))
	}
	return lines
}

func buildVersionMeaning(info map[string]any) []string {
	out := []string{
		fmt.Sprintf("Authentication is currently using %s.", blankDash(stringValue(info["auth_mode"]))),
	}
	if strings.TrimSpace(stringValue(info["tenant"])) == "" {
		out = append(out, "No tenant override is set for this session.")
	} else {
		out = append(out, fmt.Sprintf("Commands will default to tenant %s unless overridden.", stringValue(info["tenant"])))
	}
	return out
}

func versionSuggestions(info map[string]any) []string {
	suggestions := []string{"kubectl cybermesh doctor"}
	if strings.TrimSpace(stringValue(info["base_url"])) != "" {
		suggestions = append(suggestions, "kubectl cybermesh consensus")
	}
	return suggestions
}

func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", x)
	}
}

type doctorView struct {
	SummaryFields []detailField
	Timeline      []string
	Meaning       []string
	Snapshot      []doctorCheck
}

func buildDoctorView(resp doctorResult) doctorView {
	return doctorView{
		SummaryFields: []detailField{
			{Key: "Version", Value: blankDash(resp.Version)},
			{Key: "Checks", Value: fmt.Sprintf("%d", len(resp.Checks))},
			{Key: "Passing", Value: fmt.Sprintf("%d", countDoctorStatus(resp.Checks, "ok"))},
			{Key: "Failing", Value: fmt.Sprintf("%d", countDoctorStatus(resp.Checks, "fail"))},
			{Key: "Other Statuses", Value: fmt.Sprintf("%d", countOtherDoctorStatuses(resp.Checks))},
		},
		Timeline: buildDoctorTimeline(resp),
		Meaning:  buildDoctorMeaning(resp),
		Snapshot: compactDoctorSnapshot(resp.Checks, 5),
	}
}

func renderDoctorSummary(w io.Writer, view doctorView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, item := range view.Meaning {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.Snapshot) > 0 {
		renderSubTitle(w, "Check Snapshot")
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "CHECK\tSTATUS\tDETAILS")
		for _, check := range view.Snapshot {
			fmt.Fprintf(tw, "%s\t%s\t%s\n",
				blankDash(check.Name),
				renderStatus(check.Status),
				blankDash(check.Details),
			)
		}
		_ = tw.Flush()
	}
}

func buildDoctorTimeline(resp doctorResult) []string {
	lines := make([]string, 0, len(resp.Checks)+1)
	lines = append(lines, fmt.Sprintf("Doctor evaluated %d checks for kubectl-cybermesh %s.", len(resp.Checks), blankDash(resp.Version)))
	for _, check := range resp.Checks {
		lines = append(lines, fmt.Sprintf("Check %s reported %s.", blankDash(check.Name), renderStatus(check.Status)))
	}
	return compactStringList(lines, 8)
}

func buildDoctorMeaning(resp doctorResult) []string {
	failures := countDoctorStatus(resp.Checks, "fail")
	passing := countDoctorStatus(resp.Checks, "ok")
	out := make([]string, 0, 3)
	switch {
	case len(resp.Checks) == 0:
		out = append(out, "No doctor checks were recorded, so connectivity and local configuration should be revalidated.")
	case failures == 0:
		out = append(out, "All recorded doctor checks are currently passing.")
	default:
		out = append(out, fmt.Sprintf("%d doctor checks are failing and need operator attention.", failures))
	}
	if passing > 0 {
		out = append(out, fmt.Sprintf("%d checks are passing in the current environment.", passing))
	}
	return out
}

func compactDoctorSnapshot(checks []doctorCheck, max int) []doctorCheck {
	if len(checks) <= max || max < 2 {
		return checks
	}
	snapshot := append([]doctorCheck{}, checks[:max]...)
	snapshot = append(snapshot, doctorCheck{
		Name:    "additional_checks",
		Status:  "info",
		Details: fmt.Sprintf("%d more checks omitted", len(checks)-max),
	})
	return snapshot
}

func countDoctorStatus(checks []doctorCheck, status string) int {
	count := 0
	for _, check := range checks {
		if strings.EqualFold(strings.TrimSpace(check.Status), status) {
			count++
		}
	}
	return count
}

func countOtherDoctorStatuses(checks []doctorCheck) int {
	count := 0
	for _, check := range checks {
		status := strings.ToLower(strings.TrimSpace(check.Status))
		if status == "" || status == "ok" || status == "fail" {
			continue
		}
		count++
	}
	return count
}

func doctorSuggestions(resp doctorResult) []string {
	suggestions := []string{"kubectl cybermesh version --verbose"}
	if countDoctorStatus(resp.Checks, "fail") > 0 {
		suggestions = append(suggestions, "kubectl cybermesh control", "kubectl cybermesh consensus")
	}
	return suggestions
}

func (r *commandRunner) renderLanding(resp landingResult) error {
	renderTitle(r.out, "CyberMesh")
	renderKV(r.out, map[string]string{
		"Safe Mode":        renderBooleanStatus(resp.Control.ControlMutationSafeMode),
		"Kill Switch":      renderBooleanStatus(resp.Control.ControlMutationKillSwitch),
		"Lease Rows":       fmt.Sprintf("%d", len(resp.Control.Leases)),
		"Pending Backlog":  fmt.Sprintf("%d", resp.Backlog.Pending),
		"Retry Backlog":    fmt.Sprintf("%d", resp.Backlog.Retry),
		"Publishing Rows":  fmt.Sprintf("%d", resp.Backlog.Publishing),
		"ACK Closure Ratio": fmt.Sprintf("%.2f", resp.Backlog.AckClosureRatio),
	})
	if len(resp.Control.Leases) > 0 {
		renderSubTitle(r.out, "Lease Snapshot")
		tw := newTabWriter(r.out)
		fmt.Fprintln(tw, "LEASE\tHOLDER\tEPOCH\tACTIVE")
		for i, row := range resp.Control.Leases {
			if i >= 3 {
				break
			}
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\n",
				blankDash(shortID(row.LeaseKey, 24)),
				blankDash(shortID(row.HolderID, 24)),
				row.Epoch,
				renderBooleanStatus(row.IsActive),
			)
		}
		_ = tw.Flush()
		if len(resp.Control.Leases) > 3 {
			_, _ = fmt.Fprintf(r.out, "... %d more lease rows ...\n", len(resp.Control.Leases)-3)
		}
	}
	renderSuggestions(r.out, []string{
		"kubectl cybermesh control",
		"kubectl cybermesh backlog",
		"kubectl cybermesh policies list --limit 10",
		"kubectl cybermesh workflows list --limit 10",
	})
	return nil
}

func (r *commandRunner) renderControlStatus(resp controlStatusResponse) error {
	renderTitle(r.out, "Control")
	renderControlSummary(r.out, buildControlView(resp))
	renderSuggestions(r.out, controlSuggestions(resp))
	return nil
}

func (r *commandRunner) renderLeases(resp controlStatusResponse) error {
	renderTitle(r.out, "Leases")
	renderLeasesSummary(r.out, buildLeasesView(resp))
	renderSuggestions(r.out, leaseSuggestions(resp))
	return nil
}

type controlView struct {
	SummaryFields []detailField
	Timeline      []string
	Meaning       []string
	LeaseSnapshot []controlLeaseRow
}

func buildControlView(resp controlStatusResponse) controlView {
	snapshot := resp.Leases
	if len(snapshot) > 3 {
		snapshot = snapshot[:3]
	}
	return controlView{
		SummaryFields: []detailField{
			{Key: "Safe Mode", Value: renderBooleanStatus(resp.ControlMutationSafeMode)},
			{Key: "Kill Switch", Value: renderBooleanStatus(resp.ControlMutationKillSwitch)},
			{Key: "Lease Rows", Value: fmt.Sprintf("%d", len(resp.Leases))},
			{Key: "Active Leases", Value: fmt.Sprintf("%d", countActiveLeases(resp.Leases))},
		},
		Timeline:      buildControlTimeline(resp),
		Meaning:       buildControlMeaning(resp),
		LeaseSnapshot: snapshot,
	}
}

type leasesView struct {
	SummaryFields []detailField
	Timeline      []string
	Meaning       []string
	LeaseSnapshot []controlLeaseRow
}

func buildLeasesView(resp controlStatusResponse) leasesView {
	snapshot := resp.Leases
	if len(snapshot) > 5 {
		snapshot = snapshot[:5]
	}
	return leasesView{
		SummaryFields: []detailField{
			{Key: "Lease Rows", Value: fmt.Sprintf("%d", len(resp.Leases))},
			{Key: "Active Leases", Value: fmt.Sprintf("%d", countActiveLeases(resp.Leases))},
			{Key: "Inactive Leases", Value: fmt.Sprintf("%d", len(resp.Leases)-countActiveLeases(resp.Leases))},
			{Key: "Latest Update", Value: formatTimestampAuto(latestLeaseUpdate(resp.Leases))},
		},
		Timeline:      buildLeasesTimeline(resp),
		Meaning:       buildLeasesMeaning(resp),
		LeaseSnapshot: snapshot,
	}
}

func renderControlSummary(w io.Writer, view controlView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, line := range view.Meaning {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", line)
		}
	}
	if len(view.LeaseSnapshot) > 0 {
		renderSubTitle(w, "Lease Snapshot")
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "LEASE\tHOLDER\tEPOCH\tACTIVE\tLEASE_UNTIL\tUPDATED_AT")
		for _, row := range view.LeaseSnapshot {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
				blankDash(shortID(row.LeaseKey, 24)),
				blankDash(shortID(row.HolderID, 24)),
				row.Epoch,
				renderBooleanStatus(row.IsActive),
				formatTimestampAuto(row.LeaseUntil),
				formatTimestampAuto(row.UpdatedAt),
			)
		}
		_ = tw.Flush()
	}
}

func renderLeasesSummary(w io.Writer, view leasesView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, line := range view.Meaning {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", line)
		}
	}
	if len(view.LeaseSnapshot) > 0 {
		renderSubTitle(w, "Lease Snapshot")
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "LEASE\tHOLDER\tEPOCH\tACTIVE\tLEASE_UNTIL\tUPDATED_AT")
		for _, row := range view.LeaseSnapshot {
			fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\t%s\n",
				blankDash(shortID(row.LeaseKey, 24)),
				blankDash(shortID(row.HolderID, 24)),
				row.Epoch,
				renderBooleanStatus(row.IsActive),
				formatTimestampAuto(row.LeaseUntil),
				formatTimestampAuto(row.UpdatedAt),
			)
		}
		_ = tw.Flush()
	}
}

func buildControlTimeline(resp controlStatusResponse) []string {
	lines := make([]string, 0, 4)
	switch {
	case resp.ControlMutationKillSwitch:
		lines = append(lines, "Kill switch is enabled, so control mutations are blocked.")
	case resp.ControlMutationSafeMode:
		lines = append(lines, "Safe mode is enabled, so risky control mutations require operator review.")
	default:
		lines = append(lines, "Control mutations are currently open under normal gate settings.")
	}
	active := countActiveLeases(resp.Leases)
	if active == 0 {
		lines = append(lines, "No active dispatcher leases are currently reported.")
	} else {
		lines = append(lines, fmt.Sprintf("%d active dispatcher lease rows are currently reported.", active))
	}
	if latest := latestLeaseUpdate(resp.Leases); latest > 0 {
		lines = append(lines, "Latest lease update was recorded at "+formatTimestampAuto(latest)+".")
	}
	return lines
}

func buildLeasesTimeline(resp controlStatusResponse) []string {
	lines := make([]string, 0, 3)
	active := countActiveLeases(resp.Leases)
	if len(resp.Leases) == 0 {
		return []string{"No lease rows are currently reported."}
	}
	lines = append(lines, fmt.Sprintf("%d lease rows are currently reported.", len(resp.Leases)))
	if active == 0 {
		lines = append(lines, "No active lease holders are currently reported.")
	} else {
		lines = append(lines, fmt.Sprintf("%d lease rows are currently active.", active))
	}
	if latest := latestLeaseUpdate(resp.Leases); latest > 0 {
		lines = append(lines, "Latest lease update was recorded at "+formatTimestampAuto(latest)+".")
	}
	return lines
}

func buildControlMeaning(resp controlStatusResponse) []string {
	out := make([]string, 0, 4)
	switch {
	case resp.ControlMutationKillSwitch:
		out = append(out, "The kill switch is active, so write-side control actions should be treated as blocked until it is disabled.")
	case resp.ControlMutationSafeMode:
		out = append(out, "Safe mode is active, so mutation flows should be reviewed carefully before retrying operator actions.")
	default:
		out = append(out, "No global control gate is currently blocking normal mutation flows.")
	}
	if len(resp.Leases) == 0 {
		out = append(out, "No dispatcher lease rows were returned, so lease ownership should be checked before assuming active processing.")
	} else {
		out = append(out, fmt.Sprintf("The control plane currently reports %d lease rows.", len(resp.Leases)))
	}
	return out
}

func buildLeasesMeaning(resp controlStatusResponse) []string {
	out := make([]string, 0, 3)
	active := countActiveLeases(resp.Leases)
	switch {
	case len(resp.Leases) == 0:
		out = append(out, "No lease rows were returned, so dispatcher ownership should be checked before assuming active control processing.")
	case active == 0:
		out = append(out, "Lease rows exist, but none are currently active.")
	default:
		out = append(out, fmt.Sprintf("%d active lease rows are currently reported.", active))
	}
	if resp.ControlMutationKillSwitch || resp.ControlMutationSafeMode {
		out = append(out, "Control gate flags are also present in the lease view for operator context.")
	}
	return out
}

func controlSuggestions(resp controlStatusResponse) []string {
	suggestions := []string{"kubectl cybermesh backlog", "kubectl cybermesh leases"}
	if resp.ControlMutationKillSwitch {
		suggestions = append(suggestions, "kubectl cybermesh kill-switch disable --reason-code <code> --reason-text <text> --yes")
	} else if resp.ControlMutationSafeMode {
		suggestions = append(suggestions, "kubectl cybermesh safe-mode disable --reason-code <code> --reason-text <text> --yes")
	}
	return suggestions
}

func leaseSuggestions(resp controlStatusResponse) []string {
	suggestions := []string{"kubectl cybermesh control", "kubectl cybermesh backlog"}
	if countActiveLeases(resp.Leases) == 0 {
		suggestions = append(suggestions, "kubectl cybermesh doctor")
	}
	return suggestions
}

func countActiveLeases(rows []controlLeaseRow) int {
	count := 0
	for _, row := range rows {
		if row.IsActive {
			count++
		}
	}
	return count
}

func latestLeaseUpdate(rows []controlLeaseRow) int64 {
	var latest int64
	for _, row := range rows {
		if row.UpdatedAt > latest {
			latest = row.UpdatedAt
		}
	}
	return latest
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
	suggestions := []string{}
	if strings.TrimSpace(resp.Outbox.PolicyID) != "" {
		suggestions = append(suggestions,
			fmt.Sprintf("kubectl cybermesh policies get %s", resp.Outbox.PolicyID),
			fmt.Sprintf("kubectl cybermesh trace policy %s", resp.Outbox.PolicyID),
		)
	}
	if strings.TrimSpace(resp.WorkflowID) != "" {
		suggestions = append(suggestions,
			fmt.Sprintf("kubectl cybermesh workflows get %s", resp.WorkflowID),
			fmt.Sprintf("kubectl cybermesh audit get --workflow-id %s --limit 10", resp.WorkflowID),
		)
	} else if strings.TrimSpace(resp.Outbox.PolicyID) != "" {
		suggestions = append(suggestions,
			fmt.Sprintf("kubectl cybermesh audit get --policy-id %s --limit 10", resp.Outbox.PolicyID),
		)
	}
	renderSuggestions(r.out, suggestions)
	return nil
}

func (r *commandRunner) renderRevokeResult(resp controlMutationResponse) error {
	renderTitle(r.out, "Revoke")
	renderRevokeSummary(r.out, buildRevokeView(resp))
	renderSuggestions(r.out, revokeSuggestions(resp))
	return nil
}

type revokeView struct {
	SummaryFields []detailField
	Timeline      []string
	Meaning       []string
}

func buildRevokeView(resp controlMutationResponse) revokeView {
	return revokeView{
		SummaryFields: []detailField{
			{Key: "Action", Value: blankDash(resp.ActionType)},
			{Key: "Action ID", Value: blankDash(resp.ActionID)},
			{Key: "Command ID", Value: blankDash(resp.CommandID)},
			{Key: "Workflow ID", Value: blankDash(resp.WorkflowID)},
			{Key: "Outbox ID", Value: blankDash(resp.Outbox.ID)},
			{Key: "Policy ID", Value: blankDash(resp.Outbox.PolicyID)},
			{Key: "Status", Value: renderStatus(resp.Outbox.Status)},
			{Key: "Replay", Value: fmt.Sprintf("%t", resp.IdempotentReplay)},
		},
		Timeline: buildRevokeTimeline(resp),
		Meaning:  buildRevokeMeaning(resp),
	}
}

func renderRevokeSummary(w io.Writer, view revokeView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, item := range view.Meaning {
			if strings.TrimSpace(item) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item)
		}
	}
}

func buildRevokeTimeline(resp controlMutationResponse) []string {
	lines := []string{
		fmt.Sprintf("Revoke action %s was accepted for outbox row %s.", blankDash(resp.ActionID), blankDash(resp.Outbox.ID)),
		fmt.Sprintf("The revoke targets policy %s and is currently %s.", blankDash(resp.Outbox.PolicyID), renderStatus(resp.Outbox.Status)),
	}
	if strings.TrimSpace(resp.WorkflowID) != "" {
		lines = append(lines, fmt.Sprintf("Workflow context is %s.", resp.WorkflowID))
	}
	if resp.IdempotentReplay {
		lines = append(lines, "This response represents an idempotent replay of an earlier revoke request.")
	}
	return lines
}

func buildRevokeMeaning(resp controlMutationResponse) []string {
	out := []string{
		"The revoke request has been recorded and should be verified through policy, audit, and trace reads.",
	}
	if strings.EqualFold(strings.TrimSpace(resp.Outbox.Status), "pending") {
		out = append(out, "The outbox row is still pending, so downstream processing or ACK confirmation may not have completed yet.")
	}
	if resp.IdempotentReplay {
		out = append(out, "Because this was an idempotent replay, no new revoke intent may have been created.")
	}
	return out
}

func revokeSuggestions(resp controlMutationResponse) []string {
	suggestions := make([]string, 0, 4)
	if strings.TrimSpace(resp.Outbox.PolicyID) != "" {
		suggestions = append(suggestions,
			fmt.Sprintf("kubectl cybermesh policies get %s", resp.Outbox.PolicyID),
			fmt.Sprintf("kubectl cybermesh trace policy %s", resp.Outbox.PolicyID),
		)
	}
	if strings.TrimSpace(resp.WorkflowID) != "" {
		suggestions = append(suggestions,
			fmt.Sprintf("kubectl cybermesh workflows get %s", resp.WorkflowID),
			fmt.Sprintf("kubectl cybermesh audit get --workflow-id %s --limit 10", resp.WorkflowID),
		)
	} else if strings.TrimSpace(resp.Outbox.PolicyID) != "" {
		suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh audit get --policy-id %s --limit 10", resp.Outbox.PolicyID))
	}
	return suggestions
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
	if err := tw.Flush(); err != nil {
		return err
	}
	renderSuggestions(r.out, []string{
		fmt.Sprintf("kubectl cybermesh workflows get %s", resp.WorkflowID),
		fmt.Sprintf("kubectl cybermesh audit get --workflow-id %s --limit 10", resp.WorkflowID),
		fmt.Sprintf("kubectl cybermesh trace workflow %s", resp.WorkflowID),
	})
	return nil
}

func (r *commandRunner) renderOutboxRows(resp outboxListResponse) error {
	if len(resp.Rows) == 0 {
		_, _ = fmt.Fprintln(r.out, "No outbox rows found.")
		return nil
	}
	renderTitle(r.out, "Outbox")
	renderOutboxSummary(r.out, buildOutboxView(resp))
	renderSuggestions(r.out, outboxSuggestions(resp))
	return nil
}

type outboxView struct {
	SummaryFields []detailField
	Timeline      []policyTimelineEntry
	Latest        []detailField
	Meaning       []string
	Recent        []outboxRow
}

func buildOutboxView(resp outboxListResponse) outboxView {
	return outboxView{
		SummaryFields: []detailField{
			{Key: "Matches", Value: fmt.Sprintf("%d", len(resp.Rows))},
			{Key: "Pending", Value: fmt.Sprintf("%d", countOutboxStatus(resp.Rows, "pending"))},
			{Key: "Published", Value: fmt.Sprintf("%d", countOutboxStatus(resp.Rows, "published"))},
			{Key: "Acked", Value: fmt.Sprintf("%d", countOutboxStatus(resp.Rows, "acked"))},
			{Key: "Failed/Retry", Value: fmt.Sprintf("%d", countOutboxStatus(resp.Rows, "failed")+countOutboxStatus(resp.Rows, "retry"))},
			{Key: "Latest Workflow", Value: blankDash(latestOutboxWorkflow(resp.Rows))},
		},
		Timeline: buildOutboxTimeline(resp.Rows),
		Latest:   buildLatestOutboxFields(resp.Rows),
		Meaning:  buildOutboxMeaning(resp.Rows),
		Recent:   sortedOutboxRows(resp.Rows),
	}
}

func renderOutboxSummary(w io.Writer, view outboxView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			_, _ = fmt.Fprintf(w, "- %s  %s\n", formatTimestampAuto(item.When), item.Label)
		}
	}
	if len(view.Latest) > 0 {
		renderSubTitle(w, "Latest Delivery")
		renderFields(w, view.Latest)
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, line := range view.Meaning {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", line)
		}
	}
	if len(view.Recent) > 0 {
		renderSubTitle(w, "Recent Rows")
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "OUTBOX\tPOLICY\tSTATUS\tTRACE\tWHEN\tWORKFLOW")
		for i, row := range view.Recent {
			if i >= 5 {
				break
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
				shortID(row.ID, 18),
				shortID(row.PolicyID, 18),
				renderStatus(row.Status),
				shortID(row.TraceID, 20),
				formatTimestampAuto(row.CreatedAt),
				blankDash(shortID(row.WorkflowID, 18)),
			)
		}
		_ = tw.Flush()
	}
}

func buildOutboxTimeline(rows []outboxRow) []policyTimelineEntry {
	sorted := sortedOutboxRows(rows)
	out := make([]policyTimelineEntry, 0, len(sorted))
	for _, row := range sorted {
		label := joinParts(strings.ToLower(strings.TrimSpace(row.Status)), blankDash(row.PolicyID), blankDash(row.WorkflowID))
		if strings.TrimSpace(row.AckResult) != "" {
			label = joinParts(label, strings.ToLower(strings.TrimSpace(row.AckResult)))
		}
		out = append(out, policyTimelineEntry{When: row.CreatedAt, Label: label})
	}
	if len(out) > 8 {
		out = compactTraceTimeline(out, 8)
	}
	return out
}

func buildLatestOutboxFields(rows []outboxRow) []detailField {
	latest := latestOutboxRow(rows)
	if latest == nil {
		return nil
	}
	fields := []detailField{
		{Key: "Outbox ID", Value: latest.ID},
		{Key: "Policy ID", Value: latest.PolicyID},
		{Key: "Status", Value: renderStatus(latest.Status)},
		{Key: "Created At", Value: formatTimestampAuto(latest.CreatedAt)},
	}
	if strings.TrimSpace(latest.WorkflowID) != "" {
		fields = append(fields, detailField{Key: "Workflow", Value: latest.WorkflowID})
	}
	if strings.TrimSpace(latest.TraceID) != "" {
		fields = append(fields, detailField{Key: "Trace", Value: latest.TraceID})
	}
	if strings.TrimSpace(latest.AckResult) != "" {
		fields = append(fields, detailField{Key: "ACK Result", Value: renderStatus(latest.AckResult)})
	}
	if latest.AckedAt > 0 {
		fields = append(fields, detailField{Key: "Acked At", Value: formatTimestampAuto(latest.AckedAt)})
	}
	return fields
}

func buildOutboxMeaning(rows []outboxRow) []string {
	out := make([]string, 0, 4)
	switch {
	case len(rows) == 0:
		return out
	case countOutboxStatus(rows, "pending") > 0:
		out = append(out, fmt.Sprintf("%d outbox rows are still pending and may need dispatcher or ACK follow-up.", countOutboxStatus(rows, "pending")))
	case countOutboxStatus(rows, "published") > 0:
		out = append(out, fmt.Sprintf("%d outbox rows have been published and are waiting for terminal acknowledgement.", countOutboxStatus(rows, "published")))
	default:
		out = append(out, "The current outbox rows are in terminal or acknowledged states.")
	}
	if workflows := countDistinctOutboxField(rows, func(row outboxRow) string { return row.WorkflowID }); workflows > 0 {
		out = append(out, fmt.Sprintf("These rows span %d workflow contexts.", workflows))
	}
	if latest := latestOutboxRow(rows); latest != nil && strings.TrimSpace(latest.AckResult) != "" {
		out = append(out, fmt.Sprintf("The latest delivery recorded ACK result %s.", renderStatus(latest.AckResult)))
	}
	return out
}

func outboxSuggestions(resp outboxListResponse) []string {
	suggestions := []string{"kubectl cybermesh backlog"}
	if latest := latestOutboxRow(resp.Rows); latest != nil {
		if strings.TrimSpace(latest.PolicyID) != "" {
			suggestions = append(suggestions,
				fmt.Sprintf("kubectl cybermesh policies get %s", latest.PolicyID),
				fmt.Sprintf("kubectl cybermesh trace policy %s", latest.PolicyID),
			)
		}
		if strings.TrimSpace(latest.WorkflowID) != "" {
			suggestions = append(suggestions,
				fmt.Sprintf("kubectl cybermesh workflows get %s", latest.WorkflowID),
				fmt.Sprintf("kubectl cybermesh audit get --workflow-id %s --limit 10", latest.WorkflowID),
			)
		}
		if strings.TrimSpace(latest.TraceID) != "" {
			suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh trace trace %s", latest.TraceID))
		}
	}
	return suggestions
}

func countOutboxStatus(rows []outboxRow, status string) int {
	count := 0
	for _, row := range rows {
		if strings.EqualFold(strings.TrimSpace(row.Status), status) {
			count++
		}
	}
	return count
}

func latestOutboxRow(rows []outboxRow) *outboxRow {
	var best *outboxRow
	for i := range rows {
		row := &rows[i]
		if best == nil || row.CreatedAt > best.CreatedAt {
			best = row
		}
	}
	return best
}

func sortedOutboxRows(rows []outboxRow) []outboxRow {
	sorted := append([]outboxRow{}, rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].CreatedAt == sorted[j].CreatedAt {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].CreatedAt > sorted[j].CreatedAt
	})
	return sorted
}

func latestOutboxWorkflow(rows []outboxRow) string {
	if latest := latestOutboxRow(rows); latest != nil {
		return latest.WorkflowID
	}
	return ""
}

func countDistinctOutboxField(rows []outboxRow, pick func(outboxRow) string) int {
	seen := map[string]struct{}{}
	for _, row := range rows {
		value := strings.TrimSpace(pick(row))
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	return len(seen)
}

func (r *commandRunner) renderTrace(resp traceResponse) error {
	if resp.PolicyID == "" && len(resp.Outbox) == 0 && len(resp.Acks) == 0 {
		_, _ = fmt.Fprintln(r.out, "No trace data found.")
		return nil
	}
	renderTitle(r.out, "Trace")
	renderTraceSummary(r.out, buildTraceView(resp))
	renderSuggestions(r.out, traceSuggestions(resp))
	return nil
}

type traceView struct {
	SummaryFields []detailField
	Timeline      []policyTimelineEntry
	LatestAck     []detailField
	Materialized  []detailField
	Meaning       []string
	Related       []detailField
}

func buildTraceView(resp traceResponse) traceView {
	return traceView{
		SummaryFields: []detailField{
			{Key: "Policy ID", Value: resp.PolicyID},
			{Key: "Trace ID", Value: resp.TraceID},
			{Key: "Source Event ID", Value: resp.SourceEventID},
			{Key: "Sentinel Event ID", Value: resp.SentinelEventID},
			{Key: "Outbox Rows", Value: fmt.Sprintf("%d", len(resp.Outbox))},
			{Key: "ACK Rows", Value: fmt.Sprintf("%d", len(resp.Acks))},
			{Key: "Latest Workflow", Value: latestTraceWorkflow(resp)},
		},
		Timeline:     buildTraceTimeline(resp),
		LatestAck:    buildTraceLatestAck(resp),
		Materialized: buildTraceMaterialized(resp),
		Meaning:      buildTraceMeaning(resp),
		Related:      buildTraceRelated(resp),
	}
}

func renderTraceSummary(w io.Writer, view traceView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			if item.When > 0 {
				_, _ = fmt.Fprintf(w, "- %s  %s\n", formatTimestampAuto(item.When), item.Label)
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", item.Label)
		}
	}
	if len(view.LatestAck) > 0 {
		renderSubTitle(w, "Latest ACK")
		renderFields(w, view.LatestAck)
	}
	if len(view.Materialized) > 0 {
		renderSubTitle(w, "Materialized")
		renderFields(w, view.Materialized)
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, line := range view.Meaning {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", line)
		}
	}
	if len(view.Related) > 0 {
		renderSubTitle(w, "Related")
		renderFields(w, view.Related)
	}
}

func buildTraceTimeline(resp traceResponse) []policyTimelineEntry {
	items := make([]policyTimelineEntry, 0, len(resp.Outbox)+len(resp.Acks)*2)
	for _, row := range resp.Outbox {
		label := joinParts("outbox", strings.ToLower(strings.TrimSpace(row.Status)), strings.TrimSpace(row.WorkflowID))
		items = append(items, policyTimelineEntry{When: row.CreatedAt, Label: label})
		if row.AckedAt > 0 {
			ackLabel := "outbox ack recorded"
			if strings.TrimSpace(row.AckResult) != "" {
				ackLabel = "outbox ack " + strings.ToLower(strings.TrimSpace(row.AckResult))
			}
			items = append(items, policyTimelineEntry{When: row.AckedAt, Label: ackLabel})
		}
	}
	for _, row := range resp.Acks {
		label := joinParts("ack", strings.ToLower(strings.TrimSpace(row.Result)), strings.TrimSpace(row.ControllerInstance))
		if strings.TrimSpace(row.ErrorCode) != "" {
			label = joinParts(label, strings.TrimSpace(row.ErrorCode))
		}
		items = append(items, policyTimelineEntry{When: row.ObservedAt, Label: label})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].When == items[j].When {
			return items[i].Label < items[j].Label
		}
		return items[i].When < items[j].When
	})
	return compactTraceTimeline(items, 12)
}

func buildTraceLatestAck(resp traceResponse) []detailField {
	ack := latestTraceAck(resp)
	if ack == nil {
		return nil
	}
	fields := []detailField{
		{Key: "Result", Value: renderStatus(ack.Result)},
		{Key: "Controller", Value: ack.ControllerInstance},
		{Key: "Scope", Value: ack.ScopeIdentifier},
		{Key: "Observed At", Value: formatTimestampAuto(ack.ObservedAt)},
		{Key: "ACK Event", Value: ack.AckEventID},
	}
	if strings.TrimSpace(ack.Reason) != "" {
		fields = append(fields, detailField{Key: "Reason", Value: ack.Reason})
	}
	if strings.TrimSpace(ack.ErrorCode) != "" {
		fields = append(fields, detailField{Key: "Error", Value: ack.ErrorCode})
	}
	if strings.TrimSpace(ack.WorkflowID) != "" {
		fields = append(fields, detailField{Key: "Workflow", Value: ack.WorkflowID})
	}
	return fields
}

func buildTraceMaterialized(resp traceResponse) []detailField {
	if resp.Materialized == nil {
		return nil
	}
	first := "-"
	if resp.Materialized.FirstPolicyAck != nil {
		first = joinParts(renderStatus(resp.Materialized.FirstPolicyAck.Result), blankDash(resp.Materialized.FirstPolicyAck.ControllerInstance))
	}
	latest := "-"
	if resp.Materialized.LatestPolicyAck != nil {
		latest = joinParts(renderStatus(resp.Materialized.LatestPolicyAck.Result), blankDash(resp.Materialized.LatestPolicyAck.ControllerInstance))
	}
	fields := []detailField{
		{Key: "Trace ID", Value: resp.Materialized.TraceID},
		{Key: "Source Event ID", Value: resp.Materialized.SourceEventID},
		{Key: "Sentinel Event ID", Value: resp.Materialized.SentinelEventID},
		{Key: "First ACK", Value: first},
		{Key: "Latest ACK", Value: latest},
	}
	if resp.Materialized.SourceEventTsMs > 0 {
		fields = append(fields, detailField{Key: "Source Event At", Value: formatTimestampAuto(resp.Materialized.SourceEventTsMs)})
	}
	if resp.Materialized.AIEventTsMs > 0 {
		fields = append(fields, detailField{Key: "AI Event At", Value: formatTimestampAuto(resp.Materialized.AIEventTsMs)})
	}
	return fields
}

func buildTraceMeaning(resp traceResponse) []string {
	out := make([]string, 0, 4)
	out = append(out, fmt.Sprintf("This trace currently links %d outbox rows and %d ACK rows.", len(resp.Outbox), len(resp.Acks)))
	if ack := latestTraceAck(resp); ack != nil {
		switch strings.ToLower(strings.TrimSpace(ack.Result)) {
		case "failed":
			if strings.TrimSpace(ack.Reason) != "" {
				out = append(out, "The latest ACK reports a failure: "+ack.Reason+".")
			} else {
				out = append(out, "The latest ACK reports a failure state.")
			}
		case "applied", "acked":
			out = append(out, "The latest ACK indicates the traced state was applied downstream.")
		}
	}
	if latestWorkflow := latestTraceWorkflow(resp); strings.TrimSpace(latestWorkflow) != "" && latestWorkflow != "-" {
		out = append(out, "The latest linked workflow for this trace is "+latestWorkflow+".")
	}
	return out
}

func buildTraceRelated(resp traceResponse) []detailField {
	related := []detailField{
		{Key: "Policy", Value: resp.PolicyID},
		{Key: "Workflow", Value: latestTraceWorkflow(resp)},
	}
	if latest := latestTraceOutbox(resp); latest != nil {
		if strings.TrimSpace(latest.RequestID) != "" {
			related = append(related, detailField{Key: "Request", Value: latest.RequestID})
		}
		if strings.TrimSpace(latest.CommandID) != "" {
			related = append(related, detailField{Key: "Command", Value: latest.CommandID})
		}
		if strings.TrimSpace(latest.ID) != "" {
			related = append(related, detailField{Key: "Latest Outbox", Value: latest.ID})
		}
	}
	if ack := latestTraceAck(resp); ack != nil {
		if strings.TrimSpace(ack.AckEventID) != "" {
			related = append(related, detailField{Key: "Latest ACK Event", Value: ack.AckEventID})
		}
	}
	return related
}

func traceSuggestions(resp traceResponse) []string {
	suggestions := []string{}
	if strings.TrimSpace(resp.TraceID) != "" {
		suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh ack get --trace-id %s", resp.TraceID))
		suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh outbox get --trace-id %s", resp.TraceID))
	}
	if strings.TrimSpace(resp.PolicyID) != "" {
		suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh policies get %s", resp.PolicyID))
	}
	if workflowID := latestTraceWorkflow(resp); strings.TrimSpace(workflowID) != "" && workflowID != "-" {
		suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh workflows get %s", workflowID))
		suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh audit get --workflow-id %s --limit 10", workflowID))
	}
	return suggestions
}

func latestTraceAck(resp traceResponse) *ackRow {
	var best *ackRow
	for i := range resp.Acks {
		row := &resp.Acks[i]
		if best == nil || row.ObservedAt > best.ObservedAt {
			best = row
		}
	}
	return best
}

func latestTraceOutbox(resp traceResponse) *outboxRow {
	var best *outboxRow
	for i := range resp.Outbox {
		row := &resp.Outbox[i]
		if best == nil || row.CreatedAt > best.CreatedAt {
			best = row
		}
	}
	return best
}

func latestTraceWorkflow(resp traceResponse) string {
	if latest := latestTraceOutbox(resp); latest != nil && strings.TrimSpace(latest.WorkflowID) != "" {
		return latest.WorkflowID
	}
	if ack := latestTraceAck(resp); ack != nil && strings.TrimSpace(ack.WorkflowID) != "" {
		return ack.WorkflowID
	}
	return "-"
}

func compactTraceTimeline(items []policyTimelineEntry, max int) []policyTimelineEntry {
	if len(items) <= max || max < 4 {
		return items
	}
	head := max / 2
	tail := max - head
	omitted := len(items) - (head + tail)
	out := make([]policyTimelineEntry, 0, max+1)
	out = append(out, items[:head]...)
	out = append(out, policyTimelineEntry{Label: fmt.Sprintf("%d more events omitted", omitted)})
	out = append(out, items[len(items)-tail:]...)
	return out
}

func compactStringList(items []string, max int) []string {
	if len(items) <= max || max < 2 {
		return items
	}
	head := max / 2
	tail := max - head
	omitted := len(items) - (head + tail)
	out := make([]string, 0, max+1)
	out = append(out, items[:head]...)
	out = append(out, fmt.Sprintf("%d more events omitted", omitted))
	out = append(out, items[len(items)-tail:]...)
	return out
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
	renderWorkflowSummary(r.out, buildWorkflowDetailView(resp))
	renderSuggestions(r.out, workflowSuggestions(resp))
	return nil
}

type workflowDetailView struct {
	SummaryFields []detailField
	Timeline      []policyTimelineEntry
	LatestAck     []detailField
	Meaning       []string
	Policies      []workflowPolicySnapshot
}

type workflowPolicySnapshot struct {
	PolicyID string
	Status   string
	TraceID  string
}

func buildWorkflowDetailView(resp workflowDetailResult) workflowDetailView {
	view := workflowDetailView{
		SummaryFields: []detailField{
			{Key: "ID", Value: blankDash(resp.Summary.WorkflowID)},
			{Key: "Status", Value: workflowDisplayStatus(resp)},
			{Key: "Latest Policy", Value: blankDash(resp.Summary.LatestPolicyID)},
			{Key: "Trace", Value: blankDash(resp.Summary.LatestTraceID)},
			{Key: "Request", Value: blankDash(resp.Summary.RequestID)},
			{Key: "Command", Value: blankDash(resp.Summary.CommandID)},
			{Key: "Policies", Value: fmt.Sprintf("%d", resp.Summary.PolicyCount)},
			{Key: "Outbox Rows", Value: fmt.Sprintf("%d", resp.Summary.OutboxCount)},
			{Key: "ACK Rows", Value: fmt.Sprintf("%d", resp.Summary.AckCount)},
			{Key: "Updated At", Value: formatTimestampAuto(resp.Summary.LatestCreatedAt)},
		},
		Timeline:  buildWorkflowTimeline(resp),
		LatestAck: buildWorkflowLatestAck(resp),
		Meaning:   buildWorkflowMeaning(resp),
		Policies:  buildWorkflowPolicySnapshots(resp),
	}
	return view
}

func renderWorkflowSummary(w io.Writer, view workflowDetailView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			_, _ = fmt.Fprintf(w, "- %s  %s\n", formatTimestampAuto(item.When), item.Label)
		}
	}
	if len(view.LatestAck) > 0 {
		renderSubTitle(w, "Latest ACK")
		renderFields(w, view.LatestAck)
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, line := range view.Meaning {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", line)
		}
	}
	if len(view.Policies) > 0 {
		renderSubTitle(w, "Policy Snapshot")
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "POLICY\tSTATUS\tTRACE")
		for i, row := range view.Policies {
			if i >= 5 {
				break
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\n",
				shortID(row.PolicyID, 18),
				blankDash(row.Status),
				shortID(row.TraceID, 20),
			)
		}
		_ = tw.Flush()
		if len(view.Policies) > 5 {
			_, _ = fmt.Fprintf(w, "... %d more policies ...\n", len(view.Policies)-5)
		}
	}
}

func buildWorkflowTimeline(resp workflowDetailResult) []policyTimelineEntry {
	entries := make([]policyTimelineEntry, 0, 5)
	firstOutboxAt := int64(0)
	for _, row := range resp.Outbox {
		if row.CreatedAt > 0 && (firstOutboxAt == 0 || row.CreatedAt < firstOutboxAt) {
			firstOutboxAt = row.CreatedAt
		}
	}
	if firstOutboxAt > 0 {
		entries = append(entries, policyTimelineEntry{When: firstOutboxAt, Label: "Workflow activity recorded"})
	}
	if resp.Summary.LatestCreatedAt > 0 && resp.Summary.LatestCreatedAt != firstOutboxAt {
		entries = append(entries, policyTimelineEntry{When: resp.Summary.LatestCreatedAt, Label: "Latest workflow state recorded"})
	}
	if resp.Summary.LatestPublishedAt > 0 {
		entries = append(entries, policyTimelineEntry{When: resp.Summary.LatestPublishedAt, Label: "Latest workflow delivery published"})
	}
	if latestAck := latestWorkflowAck(resp); latestAck != nil {
		label := "Latest ACK recorded"
		if result := strings.ToLower(strings.TrimSpace(latestAck.Result)); result != "" {
			label = "Latest ACK " + result
		}
		if code := strings.TrimSpace(latestAck.ErrorCode); code != "" {
			label += ": " + code
		}
		entries = append(entries, policyTimelineEntry{When: workflowAckWhen(*latestAck), Label: label})
	} else if resp.Summary.LatestAckedAt > 0 {
		label := "Latest ACK recorded"
		if result := strings.ToLower(strings.TrimSpace(resp.Summary.LatestAckResult)); result != "" {
			label = "Latest ACK " + result
		}
		entries = append(entries, policyTimelineEntry{When: resp.Summary.LatestAckedAt, Label: label})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].When < entries[j].When
	})
	return entries
}

func buildWorkflowLatestAck(resp workflowDetailResult) []detailField {
	ack := latestWorkflowAck(resp)
	if ack == nil && strings.TrimSpace(resp.Summary.LatestAckResult) == "" && strings.TrimSpace(resp.Summary.LatestAckEventID) == "" {
		return []detailField{
			{Key: "Result", Value: "PENDING"},
			{Key: "Reason", Value: "No ACK recorded yet"},
		}
	}
	fields := make([]detailField, 0, 5)
	if ack != nil {
		fields = append(fields, detailField{Key: "Result", Value: blankDash(renderStatus(ack.Result))})
		if reason := strings.TrimSpace(ack.Reason); reason != "" {
			fields = append(fields, detailField{Key: "Reason", Value: reason})
		}
		if code := strings.TrimSpace(ack.ErrorCode); code != "" {
			fields = append(fields, detailField{Key: "Error", Value: code})
		}
		if controller := strings.TrimSpace(ack.ControllerInstance); controller != "" {
			fields = append(fields, detailField{Key: "Controller", Value: controller})
		}
		if when := workflowAckWhen(*ack); when > 0 {
			fields = append(fields, detailField{Key: "Observed At", Value: formatTimestampAuto(when)})
		}
		if eventID := strings.TrimSpace(ack.AckEventID); eventID != "" {
			fields = append(fields, detailField{Key: "Event", Value: eventID})
		}
		return fields
	}
	fields = append(fields, detailField{Key: "Result", Value: blankDash(renderStatus(resp.Summary.LatestAckResult))})
	if controller := strings.TrimSpace(resp.Summary.LatestAckController); controller != "" {
		fields = append(fields, detailField{Key: "Controller", Value: controller})
	}
	if when := resp.Summary.LatestAckedAt; when > 0 {
		fields = append(fields, detailField{Key: "Observed At", Value: formatTimestampAuto(when)})
	}
	if eventID := strings.TrimSpace(resp.Summary.LatestAckEventID); eventID != "" {
		fields = append(fields, detailField{Key: "Event", Value: eventID})
	}
	return fields
}

func buildWorkflowMeaning(resp workflowDetailResult) []string {
	out := make([]string, 0, 3)
	if resp.Summary.PolicyCount > 0 {
		out = append(out, fmt.Sprintf("This workflow currently links %d policy objects.", resp.Summary.PolicyCount))
	}
	switch strings.ToLower(strings.TrimSpace(resp.Summary.LatestStatus)) {
	case "acked":
		out = append(out, "The latest workflow-linked outbox row reached an ACKed state.")
	case "published":
		out = append(out, "The latest workflow-linked outbox row was published and is awaiting final ACK state.")
	case "pending", "publishing", "retry":
		out = append(out, fmt.Sprintf("The latest workflow-linked outbox row is still in %s.", strings.ToLower(strings.TrimSpace(resp.Summary.LatestStatus))))
	case "terminal_failed":
		out = append(out, "The latest workflow-linked outbox row reached a terminal failure state.")
	}
	if ack := latestWorkflowAck(resp); ack != nil {
		switch strings.ToLower(strings.TrimSpace(ack.Result)) {
		case "applied":
			out = append(out, "The downstream controller reported that the latest workflow state was applied.")
		case "failed":
			if reason := strings.TrimSpace(ack.Reason); reason != "" {
				out = append(out, "The downstream controller reported a failure: "+reason+".")
			} else {
				out = append(out, "The downstream controller reported a failure for the latest workflow state.")
			}
		case "rejected":
			out = append(out, "The downstream controller rejected the latest workflow state.")
		}
	} else if resp.Summary.AckCount == 0 {
		out = append(out, "No ACK history is recorded for this workflow yet.")
	}
	if len(out) == 0 {
		out = append(out, "The workflow summary is available, but there is not enough lifecycle detail to infer more yet.")
	}
	return out
}

func buildWorkflowPolicySnapshots(resp workflowDetailResult) []workflowPolicySnapshot {
	rows := make([]workflowPolicySnapshot, 0, len(resp.Policies))
	for _, row := range resp.Policies {
		rows = append(rows, workflowPolicySnapshot{
			PolicyID: row.PolicyID,
			Status:   renderStatus(row.LatestStatus),
			TraceID:  row.TraceID,
		})
	}
	return rows
}

func latestWorkflowAck(resp workflowDetailResult) *ackRow {
	var best *ackRow
	for i := range resp.Acks {
		row := &resp.Acks[i]
		if best == nil || workflowAckWhen(*row) > workflowAckWhen(*best) {
			best = row
		}
	}
	return best
}

func workflowAckWhen(row ackRow) int64 {
	if row.ObservedAt > 0 {
		return row.ObservedAt
	}
	return 0
}

func workflowDisplayStatus(resp workflowDetailResult) string {
	status := strings.TrimSpace(resp.Summary.LatestStatus)
	if ack := latestWorkflowAck(resp); ack != nil && strings.EqualFold(strings.TrimSpace(ack.Result), "failed") {
		return "DECISION FAILED"
	}
	return renderStatus(status)
}

func (r *commandRunner) renderAudit(rows []auditEntry) error {
	if len(rows) == 0 {
		_, _ = fmt.Fprintln(r.out, "No audit rows found.")
		return nil
	}
	renderTitle(r.out, "Audit")
	renderAuditSummary(r.out, buildAuditView(rows))
	renderSuggestions(r.out, auditSuggestions(rows))
	return nil
}

type auditView struct {
	SummaryFields []detailField
	Timeline      []policyTimelineEntry
	LatestAction  []detailField
	Meaning       []string
	Recent        []auditRecentEntry
}

type auditRecentEntry struct {
	ActionID    string
	ActionType  string
	TargetKind  string
	PolicyID    string
	WorkflowID  string
	Actor       string
	Reason      string
	BeforeAfter string
}

func buildAuditView(rows []auditEntry) auditView {
	view := auditView{
		SummaryFields: []detailField{
			{Key: "Matches", Value: fmt.Sprintf("%d", len(rows))},
			{Key: "Action Types", Value: fmt.Sprintf("%d", countDistinctAudit(rows, func(row auditEntry) string { return row.ActionType }))},
			{Key: "Policies", Value: fmt.Sprintf("%d", countDistinctAudit(rows, func(row auditEntry) string { return row.PolicyID }))},
			{Key: "Workflows", Value: fmt.Sprintf("%d", countDistinctAudit(rows, func(row auditEntry) string { return row.WorkflowID }))},
			{Key: "Latest Action", Value: formatTimestampAuto(latestAuditTimestamp(rows))},
		},
		Timeline:     buildAuditTimeline(rows),
		LatestAction: buildAuditLatestAction(rows),
		Meaning:      buildAuditMeaning(rows),
		Recent:       buildAuditRecentEntries(rows),
	}
	return view
}

func renderAuditSummary(w io.Writer, view auditView) {
	renderFields(w, view.SummaryFields)
	if len(view.Timeline) > 0 {
		renderSubTitle(w, "Timeline")
		for _, item := range view.Timeline {
			_, _ = fmt.Fprintf(w, "- %s  %s\n", formatTimestampAuto(item.When), item.Label)
		}
	}
	if len(view.LatestAction) > 0 {
		renderSubTitle(w, "Latest Action")
		renderFields(w, view.LatestAction)
	}
	if len(view.Meaning) > 0 {
		renderSubTitle(w, "What This Means")
		for _, line := range view.Meaning {
			if strings.TrimSpace(line) == "" {
				continue
			}
			_, _ = fmt.Fprintf(w, "- %s\n", line)
		}
	}
	if len(view.Recent) > 0 {
		renderSubTitle(w, "Recent Actions")
		for i, row := range view.Recent {
			if i >= 3 {
				break
			}
			_, _ = fmt.Fprintln(w, detailLine("Action", row.ActionID))
			_, _ = fmt.Fprintln(w, detailLine("Type", row.ActionType))
			_, _ = fmt.Fprintln(w, detailLine("Target", row.TargetKind))
			_, _ = fmt.Fprintln(w, detailLine("Reason", row.Reason))
			_, _ = fmt.Fprintln(w, detailLine("Before/After", row.BeforeAfter))
			_, _ = fmt.Fprintln(w, detailLine("Policy", row.PolicyID))
			_, _ = fmt.Fprintln(w, detailLine("Workflow", row.WorkflowID))
			_, _ = fmt.Fprintln(w, detailLine("Actor", row.Actor))
			_, _ = fmt.Fprintln(w)
		}
	}
}

func buildAuditTimeline(rows []auditEntry) []policyTimelineEntry {
	entries := make([]policyTimelineEntry, 0, len(rows))
	for _, row := range rows {
		label := joinParts(strings.ToLower(strings.TrimSpace(row.ActionType)), strings.ToLower(strings.TrimSpace(row.TargetKind)))
		if label == "-" {
			label = "audit event"
		}
		if after := strings.TrimSpace(row.AfterStatus); after != "" {
			label += " -> " + strings.ToLower(after)
		}
		entries = append(entries, policyTimelineEntry{
			When:  row.CreatedAt,
			Label: label,
		})
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].When < entries[j].When
	})
	return entries
}

func buildAuditLatestAction(rows []auditEntry) []detailField {
	latest := latestAuditEntry(rows)
	if latest == nil {
		return nil
	}
	fields := []detailField{
		{Key: "Action", Value: blankDash(latest.ActionType)},
		{Key: "Target", Value: blankDash(latest.TargetKind)},
		{Key: "When", Value: formatTimestampAuto(latest.CreatedAt)},
	}
	if reason := strings.TrimSpace(joinParts(latest.ReasonCode, latest.ReasonText)); reason != "-" {
		fields = append(fields, detailField{Key: "Reason", Value: reason})
	}
	if policy := strings.TrimSpace(latest.PolicyID); policy != "" {
		fields = append(fields, detailField{Key: "Policy", Value: policy})
	}
	if workflow := strings.TrimSpace(latest.WorkflowID); workflow != "" {
		fields = append(fields, detailField{Key: "Workflow", Value: workflow})
	}
	return fields
}

func buildAuditMeaning(rows []auditEntry) []string {
	out := make([]string, 0, 3)
	if len(rows) == 1 {
		out = append(out, "This audit query returned a single matching action.")
	} else {
		out = append(out, fmt.Sprintf("This audit query returned %d matching actions.", len(rows)))
	}
	if policies := countDistinctAudit(rows, func(row auditEntry) string { return row.PolicyID }); policies > 0 {
		out = append(out, fmt.Sprintf("The results touch %d distinct policy objects.", policies))
	}
	if workflows := countDistinctAudit(rows, func(row auditEntry) string { return row.WorkflowID }); workflows > 0 {
		out = append(out, fmt.Sprintf("The results span %d workflow contexts.", workflows))
	}
	return out
}

func buildAuditRecentEntries(rows []auditEntry) []auditRecentEntry {
	sorted := append([]auditEntry(nil), rows...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return latestAuditEntryOrder(sorted[i], sorted[j])
	})
	recent := make([]auditRecentEntry, 0, len(rows))
	for _, row := range sorted {
		recent = append(recent, auditRecentEntry{
			ActionID:    blankDash(row.ActionID),
			ActionType:  blankDash(row.ActionType),
			TargetKind:  blankDash(row.TargetKind),
			PolicyID:    blankDash(row.PolicyID),
			WorkflowID:  blankDash(row.WorkflowID),
			Actor:       blankDash(row.Actor),
			Reason:      joinParts(row.ReasonCode, row.ReasonText),
			BeforeAfter: joinParts(blankDash(renderStatus(row.BeforeStatus)), blankDash(renderStatus(row.AfterStatus))),
		})
	}
	return recent
}

func latestAuditEntry(rows []auditEntry) *auditEntry {
	if len(rows) == 0 {
		return nil
	}
	latest := &rows[0]
	for i := 1; i < len(rows); i++ {
		if rows[i].CreatedAt > latest.CreatedAt {
			latest = &rows[i]
		}
	}
	return latest
}

func latestAuditEntryOrder(a, b auditEntry) bool {
	if a.CreatedAt == b.CreatedAt {
		return a.ActionID < b.ActionID
	}
	return a.CreatedAt > b.CreatedAt
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

func renderSuggestions(w io.Writer, suggestions []string) {
	if len(suggestions) == 0 {
		return
	}
	renderSubTitle(w, "Next")
	for _, suggestion := range suggestions {
		if strings.TrimSpace(suggestion) == "" {
			continue
		}
		_, _ = fmt.Fprintf(w, "  %s\n", suggestion)
	}
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

type detailField struct {
	Key   string
	Value string
}

func renderFields(w io.Writer, fields []detailField) {
	tw := newTabWriter(w)
	for _, field := range fields {
		if strings.TrimSpace(field.Key) == "" {
			continue
		}
		fmt.Fprintf(tw, "%s\t%s\n", field.Key, blankDash(field.Value))
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

func renderTraceSnapshot(w io.Writer, resp traceResponse) {
	renderSubTitle(w, "Trace Snapshot")
	renderKV(w, map[string]string{
		"Trace ID":         blankDash(resp.TraceID),
		"Source Event ID":  blankDash(resp.SourceEventID),
		"Sentinel Event ID": blankDash(resp.SentinelEventID),
		"Outbox Rows":      fmt.Sprintf("%d", len(resp.Outbox)),
		"ACK Rows":         fmt.Sprintf("%d", len(resp.Acks)),
	})
	if resp.Materialized != nil {
		first := "-"
		if resp.Materialized.FirstPolicyAck != nil {
			first = joinParts(renderStatus(resp.Materialized.FirstPolicyAck.Result), blankDash(resp.Materialized.FirstPolicyAck.ControllerInstance))
		}
		latest := "-"
		if resp.Materialized.LatestPolicyAck != nil {
			latest = joinParts(renderStatus(resp.Materialized.LatestPolicyAck.Result), blankDash(resp.Materialized.LatestPolicyAck.ControllerInstance))
		}
		renderKV(w, map[string]string{
			"First ACK":  first,
			"Latest ACK": latest,
		})
	}
	if len(resp.Outbox) > 0 {
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "OUTBOX\tSTATUS\tREQUEST\tCOMMAND\tWORKFLOW")
		for i, row := range resp.Outbox {
			if i >= 3 {
				break
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
				shortID(row.ID, 18),
				renderStatus(row.Status),
				shortID(row.RequestID, 18),
				shortID(row.CommandID, 18),
				shortID(row.WorkflowID, 18),
			)
		}
		_ = tw.Flush()
		if len(resp.Outbox) > 3 {
			_, _ = fmt.Fprintf(w, "... %d more outbox rows ...\n", len(resp.Outbox)-3)
		}
	}
	if len(resp.Acks) > 0 {
		tw := newTabWriter(w)
		fmt.Fprintln(tw, "ACK_EVENT\tRESULT\tCONTROLLER\tWHEN")
		for i, row := range resp.Acks {
			if i >= 3 {
				break
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n",
				shortID(row.AckEventID, 18),
				renderStatus(row.Result),
				blankDash(row.ControllerInstance),
				formatTimestampAuto(row.ObservedAt),
			)
		}
		_ = tw.Flush()
		if len(resp.Acks) > 3 {
			_, _ = fmt.Fprintf(w, "... %d more ACK rows ...\n", len(resp.Acks)-3)
		}
	}
}

func joinParts(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || part == "-" {
			continue
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return "-"
	}
	return strings.Join(out, " | ")
}

func detailLine(key, value string) string {
	return fmt.Sprintf("%-16s %s", key+":", blankDash(value))
}

func policySuggestions(resp policyDetailResult) []string {
	suggestions := []string{
		fmt.Sprintf("kubectl cybermesh policies coverage %s", resp.Summary.PolicyID),
		fmt.Sprintf("kubectl cybermesh trace policy %s", resp.Summary.PolicyID),
	}
	if strings.TrimSpace(resp.Summary.WorkflowID) != "" {
		suggestions = append(suggestions,
			fmt.Sprintf("kubectl cybermesh workflows get %s", resp.Summary.WorkflowID),
			fmt.Sprintf("kubectl cybermesh audit get --workflow-id %s --limit 10", resp.Summary.WorkflowID),
		)
	} else {
		suggestions = append(suggestions,
			fmt.Sprintf("kubectl cybermesh audit get --policy-id %s --limit 10", resp.Summary.PolicyID),
		)
	}
	return suggestions
}

func workflowSuggestions(resp workflowDetailResult) []string {
	suggestions := []string{
		fmt.Sprintf("kubectl cybermesh audit get --workflow-id %s --limit 10", resp.Summary.WorkflowID),
		fmt.Sprintf("kubectl cybermesh trace workflow %s", resp.Summary.WorkflowID),
	}
	if strings.TrimSpace(resp.Summary.LatestPolicyID) != "" {
		suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh policies get %s", resp.Summary.LatestPolicyID))
	}
	return suggestions
}

func auditSuggestions(rows []auditEntry) []string {
	if len(rows) == 0 {
		return nil
	}
	first := rows[0]
	suggestions := make([]string, 0, 4)
	if strings.TrimSpace(first.PolicyID) != "" {
		suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh policies get %s", first.PolicyID))
	}
	if strings.TrimSpace(first.WorkflowID) != "" {
		suggestions = append(suggestions,
			fmt.Sprintf("kubectl cybermesh workflows get %s", first.WorkflowID),
			fmt.Sprintf("kubectl cybermesh trace workflow %s", first.WorkflowID),
		)
	}
	if len(suggestions) == 0 && strings.TrimSpace(first.RequestID) != "" {
		suggestions = append(suggestions, fmt.Sprintf("kubectl cybermesh ack get --request-id %s", first.RequestID))
	}
	return suggestions
}

func countDistinctAudit(rows []auditEntry, pick func(auditEntry) string) int {
	seen := map[string]struct{}{}
	for _, row := range rows {
		value := strings.TrimSpace(pick(row))
		if value == "" {
			continue
		}
		seen[value] = struct{}{}
	}
	return len(seen)
}

func latestAuditTimestamp(rows []auditEntry) int64 {
	var latest int64
	for _, row := range rows {
		if row.CreatedAt > latest {
			latest = row.CreatedAt
		}
	}
	return latest
}
