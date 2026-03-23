package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type policiesModel struct {
	ctx           context.Context
	title         string
	listFetch     policyListFetcher
	detailFetch   policyDetailFetcher
	coverageFetch policyCoverageFetcher
	workflowFetch workflowDetailFetcher
	auditFetch    policyAuditFetcher
	mutationFetch policyMutationFetcher
	autoExit      bool
	loading       bool
	rows          []policySummary
	selected      int
	detail        policyDetailResult
	coverage      policyCoverageResult
	err           error
	pendingDraft  *actionDraft
	statusMessage string
	initialPolicy string
	smokeAction   string
	smokeDone     bool
	showWorkflow  bool
	showTrace     bool
	showAudit     bool
	linkedWork    workflowDetailResult
	linkedWorkID  string
	linkedAudit   auditListResult
	detailMode    bool
	showHelp      bool
	scrollOffset  int
	width         int
	height        int
}

func newPoliciesModel(ctx context.Context, title string, initialPolicy string, listFetch policyListFetcher, detailFetch policyDetailFetcher, coverageFetch policyCoverageFetcher, workflowFetch workflowDetailFetcher, auditFetch policyAuditFetcher, mutationFetch policyMutationFetcher) tea.Model {
	width, height := initialTerminalSize()
	return &policiesModel{
		ctx:           ctx,
		title:         title,
		initialPolicy: strings.TrimSpace(initialPolicy),
		listFetch:     listFetch,
		detailFetch:   detailFetch,
		coverageFetch: coverageFetch,
		workflowFetch: workflowFetch,
		auditFetch:    auditFetch,
		mutationFetch: mutationFetch,
		autoExit:      tuiAutoExitEnabled(),
		loading:       true,
		smokeAction:   tuiSmokeAction(),
		showTrace:     true,
		width:         width,
		height:        height,
	}
}

func (m *policiesModel) Init() tea.Cmd {
	return asyncPolicyListLoad(m.ctx, m.listFetch)
}

func (m *policiesModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		if m.showHelp {
			switch msg.String() {
			case "?", "esc":
				m.showHelp = false
			}
			return m, nil
		}
		if m.pendingDraft != nil {
			if m.pendingDraft.handleKey(msg) {
				return m, nil
			}
			switch msg.String() {
			case "esc", "n":
				m.pendingDraft = nil
				return m, nil
			case "enter", "y":
				if m.selectedPolicyID() == "" {
					m.pendingDraft = nil
					return m, nil
				}
				draft := *m.pendingDraft
				m.pendingDraft = nil
				m.statusMessage = "Submitting " + draft.Action + "..."
				return m, asyncPolicyMutation(m.ctx, m.mutationFetch, draft.Action, m.selectedPolicyID(), m.selectedWorkflowID(), draft)
			}
			return m, nil
		}
		if handleViewportKey(msg, m.height, &m.scrollOffset) {
			return m, nil
		}
		switch msg.String() {
		case "?":
			m.showHelp = true
			return m, nil
		case "ctrl+c", "q":
			return m, tea.Quit
		case "r":
			m.loading = true
			m.err = nil
			m.statusMessage = ""
			m.scrollOffset = 0
			return m, asyncPolicyListLoad(m.ctx, m.listFetch)
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.scrollOffset = 0
				return m, m.loadSelection()
			}
		case "down", "j":
			if m.selected < len(m.rows)-1 {
				m.selected++
				m.scrollOffset = 0
				return m, m.loadSelection()
			}
		case "a":
			if m.selectedPolicyID() != "" {
				draft := newActionDraft("approve")
				m.pendingDraft = &draft
			}
		case "x":
			if m.selectedPolicyID() != "" {
				draft := newActionDraft("reject")
				m.pendingDraft = &draft
			}
		case "v":
			if m.selectedPolicyID() != "" {
				draft := newActionDraft("revoke")
				m.pendingDraft = &draft
			}
		case "enter":
			m.detailMode = !m.detailMode
			m.scrollOffset = 0
			if m.detailMode {
				return m, m.loadLinkedContext(false)
			}
		case "o":
			m.detailMode = true
			m.showWorkflow = true
			m.showTrace = true
			m.showAudit = true
			m.scrollOffset = 0
			return m, m.loadLinkedContext(true)
		case "w":
			if m.selectedWorkflowID() != "" {
				m.showWorkflow = !m.showWorkflow
				if m.showWorkflow && m.linkedWorkID != m.selectedWorkflowID() {
					return m, asyncWorkflowDetailLoad(m.ctx, m.workflowFetch, m.selectedWorkflowID())
				}
			}
		case "t":
			m.showTrace = !m.showTrace
		case "u":
			m.showAudit = !m.showAudit
			if m.showAudit && m.auditFetch != nil && m.selectedPolicyID() != "" {
				return m, asyncAuditLoad(m.ctx, func(ctx context.Context) (auditListResult, error) {
					return m.auditFetch(ctx, m.selectedPolicyID())
				})
			}
		}
	case policyListLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.rows = msg.data.Rows
			if m.initialPolicy != "" {
				for i, row := range m.rows {
					if row.PolicyID == m.initialPolicy {
						m.selected = i
						break
					}
				}
			}
			if m.selected >= len(m.rows) {
				m.selected = maxInt(0, len(m.rows)-1)
			}
			if len(m.rows) > 0 {
				cmd := m.loadSelection()
				if m.autoExit && tuiSnapshotEnabled() && m.smokeAction == "" {
					return m, tea.Batch(cmd, tea.Quit)
				}
				return m, cmd
			}
		}
		if m.autoExit {
			return m, tea.Quit
		}
	case policyDetailLoadedMsg:
		if msg.err == nil {
			m.detail = msg.data
		} else {
			m.err = msg.err
		}
	case workflowDetailLoadedMsg:
		if msg.err == nil {
			m.linkedWork = msg.data
			m.linkedWorkID = msg.data.Summary.WorkflowID
		} else {
			m.err = msg.err
		}
	case policyCoverageLoadedMsg:
		if msg.err == nil {
			m.coverage = msg.data
		} else {
			m.err = msg.err
		}
		if !m.smokeDone && m.smokeAction != "" && m.selectedPolicyID() != "" {
			switch m.smokeAction {
			case "approve", "reject", "revoke":
				m.smokeDone = true
				m.statusMessage = "Submitting " + m.smokeAction + "..."
				return m, asyncPolicyMutation(m.ctx, m.mutationFetch, m.smokeAction, m.selectedPolicyID(), m.selectedWorkflowID(), newActionDraft(m.smokeAction))
			}
		}
	case auditLoadedMsg:
		if msg.err == nil {
			m.linkedAudit = msg.data
		} else {
			m.err = msg.err
		}
	case policyMutationLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.statusMessage = msg.action + " failed"
			if m.autoExit {
				return m, tea.Quit
			}
			return m, nil
		}
		m.statusMessage = fmt.Sprintf("%s submitted: %s", strings.ToUpper(msg.action), msg.data.Outbox.ID)
		if m.autoExit {
			return m, tea.Quit
		}
		return m, asyncPolicyListLoad(m.ctx, m.listFetch)
	}
	return m, nil
}

func (m *policiesModel) loadSelection() tea.Cmd {
	policyID := m.selectedPolicyID()
	if policyID == "" {
		return nil
	}
	return tea.Batch(
		asyncPolicyDetailLoad(m.ctx, m.detailFetch, policyID),
		asyncPolicyCoverageLoad(m.ctx, m.coverageFetch, policyID),
	)
}

func (m *policiesModel) loadLinkedContext(openAll bool) tea.Cmd {
	cmds := []tea.Cmd{}
	if workflowID := m.selectedWorkflowID(); workflowID != "" {
		if openAll {
			m.showWorkflow = true
		}
		if m.showWorkflow && m.linkedWorkID != workflowID {
			cmds = append(cmds, asyncWorkflowDetailLoad(m.ctx, m.workflowFetch, workflowID))
		}
	}
	if m.auditFetch != nil && m.selectedPolicyID() != "" {
		if openAll {
			m.showAudit = true
		}
		if m.showAudit {
			cmds = append(cmds, asyncAuditLoad(m.ctx, func(ctx context.Context) (auditListResult, error) {
				return m.auditFetch(ctx, m.selectedPolicyID())
			}))
		}
	}
	if openAll {
		m.showTrace = true
	}
	return tea.Batch(cmds...)
}

func (m *policiesModel) selectedPolicyID() string {
	if len(m.rows) == 0 || m.selected < 0 || m.selected >= len(m.rows) {
		return ""
	}
	return m.rows[m.selected].PolicyID
}

func (m *policiesModel) selectedWorkflowID() string {
	if len(m.rows) == 0 || m.selected < 0 || m.selected >= len(m.rows) {
		return ""
	}
	return strings.TrimSpace(m.rows[m.selected].WorkflowID)
}

func (m *policiesModel) View() string {
	compact := compactLayout(m.width)
	tiny := tinyLayout(m.width)
	header := []string{
		titleStyle.Render("CyberMesh Policies"),
		helpStyle.Render(policyHelpText(compact, m.detailMode)),
		"",
		kvLine("View", m.title),
		kvLine("Selected", m.selectedPolicyID()),
	}
	if m.statusMessage != "" {
		header = append(header, helpStyle.Render(m.statusMessage))
	}
	if m.loading {
		return joinLines(append(header, "", "Loading policies...")...)
	}
	if m.showHelp {
		return renderHelpOverlay("Policies Help", []string{
			"j/k or arrows: move selection",
			"enter: open or close expanded detail",
			"o: open linked workflow, trace, and audit context",
			"w: toggle linked workflow summary",
			"t: toggle linked trace pane",
			"u: toggle recent audit pane",
			"a: approve selected policy",
			"x: reject selected policy",
			"v: revoke selected policy",
			"PgUp / PgDn: scroll the full detail body",
			"Ctrl+U / Ctrl+D: faster full-body scroll",
			"r: refresh",
			"q: quit",
		}, m.height)
	}
	if m.err != nil {
		header = append(header, "", errorStyle.Render(m.err.Error()))
	}
	body := []string{}
	if m.detailMode {
		body = append(body, "")
		body = append(body, labelStyle.Render("Expanded Detail"))
		body = append(body, kvLine("Policy", m.detail.Summary.PolicyID))
		body = append(body, kvLine("Status", renderStatus(m.detail.Summary.LatestStatus)))
		body = append(body, kvLine("Workflow", m.detail.Summary.WorkflowID))
		body = append(body, kvLine("Trace", m.detail.Summary.TraceID))
		body = append(body, kvLine("Request", m.detail.Summary.RequestID))
		body = append(body, kvLine("Command", m.detail.Summary.CommandID))
		body = append(body, kvLine("Ack Event", m.detail.Summary.LatestAckEventID))
		body = append(body, kvLine("Ack Result", renderStatus(m.detail.Summary.LatestAckResult)))
		body = append(body, kvLine("Ack Controller", m.detail.Summary.LatestAckController))
		body = append(body, kvLine("Anomaly", m.detail.Summary.AnomalyID))
		body = append(body, kvLine("Flow", m.detail.Summary.FlowID))
		body = append(body, kvLine("Source", m.detail.Summary.SourceID))
		body = append(body, kvLine("Source Type", m.detail.Summary.SourceType))
		body = append(body, kvLine("Scope", m.detail.Summary.ScopeIdentifier))
		if len(m.detail.Trace.Outbox) > 0 {
			row := m.detail.Trace.Outbox[0]
			body = append(body, "")
			body = append(body, labelStyle.Render("Latest Outbox"))
			body = append(body, kvLine("Outbox", row.ID))
			body = append(body, kvLine("Status", renderStatus(row.Status)))
			body = append(body, kvLine("Created", formatTimestampAuto(row.CreatedAt)))
			body = append(body, kvLine("Workflow", row.WorkflowID))
		}
		if len(m.detail.Trace.Acks) > 0 {
			row := m.detail.Trace.Acks[0]
			body = append(body, "")
			body = append(body, labelStyle.Render("Latest ACK"))
			body = append(body, kvLine("Ack Event", row.AckEventID))
			body = append(body, kvLine("Result", renderStatus(row.Result)))
			body = append(body, kvLine("Controller", row.ControllerInstance))
			body = append(body, kvLine("Scope", row.ScopeIdentifier))
			body = append(body, kvLine("Observed", formatTimestampAuto(row.ObservedAt)))
		}
		if m.showTrace {
			body = append(body, "")
			body = append(body, labelStyle.Render("Linked Trace"))
			body = append(body, kvLine("Trace Policy", m.detail.Trace.PolicyID))
			body = append(body, kvLine("Trace ID", m.detail.Trace.TraceID))
			body = append(body, kvLine("Source Event", m.detail.Trace.SourceEventID))
			body = append(body, kvLine("Sentinel Event", m.detail.Trace.SentinelEventID))
			body = append(body, kvLine("Trace Outbox", fmt.Sprintf("%d", len(m.detail.Trace.Outbox))))
			body = append(body, kvLine("Trace ACKs", fmt.Sprintf("%d", len(m.detail.Trace.Acks))))
			for _, row := range m.detail.Trace.Outbox[:minInt(len(m.detail.Trace.Outbox), 3)] {
				body = append(body, fmt.Sprintf("outbox %s  %s", shortID(row.ID, 16), renderStatus(row.Status)))
			}
			for _, row := range m.detail.Trace.Acks[:minInt(len(m.detail.Trace.Acks), 3)] {
				body = append(body, fmt.Sprintf("ack %s  %s  %s", shortID(row.AckEventID, 16), renderStatus(row.Result), blankDash(shortID(row.ControllerInstance, 16))))
			}
		}
		if m.linkedWorkID != "" {
			body = append(body, "")
			body = append(body, labelStyle.Render("Linked Workflow"))
			body = append(body, kvLine("Workflow", m.linkedWork.Summary.WorkflowID))
			body = append(body, kvLine("Latest Policy", m.linkedWork.Summary.LatestPolicyID))
			body = append(body, kvLine("Latest Status", renderStatus(m.linkedWork.Summary.LatestStatus)))
			body = append(body, kvLine("Latest Trace", m.linkedWork.Summary.LatestTraceID))
			body = append(body, kvLine("Policies", fmt.Sprintf("%d", len(m.linkedWork.Policies))))
			body = append(body, kvLine("ACK Rows", fmt.Sprintf("%d", len(m.linkedWork.Acks))))
		}
		if m.showAudit && len(m.linkedAudit.Rows) > 0 {
			body = append(body, "")
			body = append(body, labelStyle.Render("Recent Audit"))
			for _, row := range m.linkedAudit.Rows[:minInt(len(m.linkedAudit.Rows), 5)] {
				body = append(body, fmt.Sprintf("%s  %s  %s", shortID(row.ActionID, 14), strings.ToUpper(blankDash(row.ActionType)), formatTimestampAuto(row.CreatedAt)))
			}
		}
		return finalizeView(header, body, m.height, m.scrollOffset)
	}
	body = append(body, "")
	body = append(body, renderPolicyListPane(m.rows, m.selected, listRowsForHeight(m.height), compact)...)
	body = append(body, "")
	body = append(body, labelStyle.Render("Policy Details"))
	body = append(body, kvLine("Status", renderStatus(m.detail.Summary.LatestStatus)))
	body = append(body, kvLine("Workflow", m.detail.Summary.WorkflowID))
	body = append(body, kvLine("Trace", m.detail.Summary.TraceID))
	body = append(body, kvLine("Ack Result", renderStatus(m.detail.Summary.LatestAckResult)))
	body = append(body, kvLine("Latest Outbox", m.detail.Summary.LatestOutboxID))
	body = append(body, kvLine("Latest Ack Event", m.detail.Summary.LatestAckEventID))
	body = append(body, kvLine("Ack Controller", m.detail.Summary.LatestAckController))
	body = append(body, kvLine("Outbox Rows", fmt.Sprintf("%d", m.detail.Summary.OutboxCount)))
	body = append(body, kvLine("ACK Rows", fmt.Sprintf("%d", m.detail.Summary.AckCount)))
	if !compact {
		body = append(body, kvLine("Anomaly", m.detail.Summary.AnomalyID))
		body = append(body, kvLine("Source", m.detail.Summary.SourceID))
		body = append(body, kvLine("Source Type", m.detail.Summary.SourceType))
		body = append(body, kvLine("Scope", m.detail.Summary.ScopeIdentifier))
	}
	body = append(body, "")
	body = append(body, labelStyle.Render("Coverage"))
	body = append(body, kvLine("Published/Acked", fmt.Sprintf("%d", m.coverage.PublishedOrAckedCount)))
	body = append(body, kvLine("Pending", fmt.Sprintf("%d", m.coverage.PendingCount)))
	body = append(body, kvLine("Retry", fmt.Sprintf("%d", m.coverage.RetryCount)))
	body = append(body, kvLine("Ack Ratio", fmt.Sprintf("%.2f", m.coverage.AckCoverageRatio)))
	if m.showTrace && !tiny {
		body = append(body, "")
		body = append(body, labelStyle.Render("Trace Summary"))
		body = append(body, kvLine("Trace Policy", m.detail.Trace.PolicyID))
		body = append(body, kvLine("Trace ID", m.detail.Trace.TraceID))
		if !compact {
			body = append(body, kvLine("Source Event", m.detail.Trace.SourceEventID))
			body = append(body, kvLine("Sentinel Event", m.detail.Trace.SentinelEventID))
		}
		body = append(body, kvLine("Trace Outbox", fmt.Sprintf("%d", len(m.detail.Trace.Outbox))))
		body = append(body, kvLine("Trace ACKs", fmt.Sprintf("%d", len(m.detail.Trace.Acks))))
	}
	if m.showWorkflow && m.linkedWorkID != "" && !tiny {
		body = append(body, "")
		body = append(body, labelStyle.Render("Linked Workflow"))
		body = append(body, kvLine("Workflow", m.linkedWork.Summary.WorkflowID))
		body = append(body, kvLine("Latest Policy", m.linkedWork.Summary.LatestPolicyID))
		body = append(body, kvLine("Latest Status", renderStatus(m.linkedWork.Summary.LatestStatus)))
		body = append(body, kvLine("Policies", fmt.Sprintf("%d", len(m.linkedWork.Policies))))
		body = append(body, kvLine("ACK Rows", fmt.Sprintf("%d", len(m.linkedWork.Acks))))
		if !compact {
			body = append(body, kvLine("Latest Ack Result", renderStatus(m.linkedWork.Summary.LatestAckResult)))
			body = append(body, kvLine("Latest Trace", m.linkedWork.Summary.LatestTraceID))
		}
	}
	if m.showAudit && len(m.linkedAudit.Rows) > 0 && !tiny {
		body = append(body, "")
		body = append(body, labelStyle.Render("Recent Audit"))
		for _, row := range m.linkedAudit.Rows[:minInt(len(m.linkedAudit.Rows), 3)] {
			body = append(body, fmt.Sprintf("%s  %s  %s", shortID(row.ActionID, 14), strings.ToUpper(blankDash(row.ActionType)), formatTimestampAuto(row.CreatedAt)))
		}
	}
	if m.pendingDraft != nil {
		body = append(body, "")
		body = append(body, errorStyle.Render("Confirm "+strings.ToUpper(m.pendingDraft.Action)))
		body = append(body, fmt.Sprintf("Policy %s", blankDash(m.selectedPolicyID())))
		body = append(body, kvLine("Current Status", renderStatus(m.detail.Summary.LatestStatus)))
		body = append(body, kvLine("Workflow", m.detail.Summary.WorkflowID))
		body = append(body, kvLine("Latest Ack", renderStatus(m.detail.Summary.LatestAckResult)))
		body = append(body, kvLine("Latest Outbox", m.detail.Summary.LatestOutboxID))
		body = append(body, kvLine(fieldCursor("reason_code", m.pendingDraft.Focus == 0), m.pendingDraft.ReasonCode))
		body = append(body, kvLine(fieldCursor("reason_text", m.pendingDraft.Focus == 1), m.pendingDraft.ReasonText))
		body = append(body, kvLine(fieldCursor("classification", m.pendingDraft.Focus == 2), m.pendingDraft.Classification))
		body = append(body, helpStyle.Render("tab switch field • type to edit • enter/y confirm • esc/n cancel"))
	}
	return finalizeView(header, body, m.height, m.scrollOffset)
}

func policyHelpText(compact bool, detailMode bool) string {
	if detailMode {
		if compact {
			return "j/k move • ? help • enter close • pg scroll • o linked • t/u panes • a/x/v act • q quit"
		}
		return "j/k move • ? help • enter close detail • pgup/pgdn scroll • o open linked • t trace • u audit • a approve • x reject • v revoke • q quit"
	}
	if compact {
		return "j/k move • ? help • enter detail • a/x/v act • w/t/u/o linked • q quit"
	}
	return "j/k move • ? help • enter detail • r refresh • a approve • x reject • v revoke • w workflow • t trace • u audit • o open linked • q quit"
}

func fieldCursor(label string, active bool) string {
	if active {
		return "> " + label
	}
	return label
}

func renderPolicyListPane(rows []policySummary, selected int, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render("Policies")}
	if len(rows) == 0 {
		return append(lines, "- no rows -")
	}
	start, end := listWindow(len(rows), selected, maxRows)
	if start > 0 {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d earlier rows ...", start)))
	}
	for i := start; i < end; i++ {
		row := rows[i]
		prefix := " "
		if i == selected {
			prefix = cursorStyle.String()
		}
		if compact {
			lines = append(lines, fmt.Sprintf("%s %s  %s", prefix, shortID(row.PolicyID, 16), renderStatus(row.LatestStatus)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  %s", prefix, shortID(row.PolicyID, 18), renderStatus(row.LatestStatus), blankDash(shortID(row.WorkflowID, 18))))
	}
	if end < len(rows) {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more rows ...", len(rows)-end)))
	}
	return lines
}
