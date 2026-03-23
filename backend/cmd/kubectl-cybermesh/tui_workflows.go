package main

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type workflowsModel struct {
	ctx            context.Context
	title          string
	listFetch      workflowListFetcher
	detailFetch    workflowDetailFetcher
	policyFetch    policyDetailFetcher
	auditFetch     workflowAuditFetcher
	rollbackFetch  workflowRollbackFetcher
	autoExit       bool
	loading        bool
	rows           []workflowSummary
	selected       int
	detail         workflowDetailResult
	err            error
	pendingDraft   *actionDraft
	statusMessage  string
	initialID      string
	smokeAction    string
	smokeDone      bool
	showPolicy     bool
	showTrace      bool
	showAudit      bool
	linkedPolicy   policyDetailResult
	linkedPolicyID string
	linkedAudit    auditListResult
	detailMode     bool
	showHelp       bool
	scrollOffset   int
	width          int
	height         int
}

func newWorkflowsModel(ctx context.Context, title string, initialID string, listFetch workflowListFetcher, detailFetch workflowDetailFetcher, policyFetch policyDetailFetcher, auditFetch workflowAuditFetcher, rollbackFetch workflowRollbackFetcher) tea.Model {
	width, height := initialTerminalSize()
	return &workflowsModel{
		ctx:           ctx,
		title:         title,
		initialID:     strings.TrimSpace(initialID),
		listFetch:     listFetch,
		detailFetch:   detailFetch,
		policyFetch:   policyFetch,
		auditFetch:    auditFetch,
		rollbackFetch: rollbackFetch,
		autoExit:      tuiAutoExitEnabled(),
		loading:       true,
		smokeAction:   tuiSmokeAction(),
		width:         width,
		height:        height,
	}
}

func (m *workflowsModel) Init() tea.Cmd {
	return asyncWorkflowListLoad(m.ctx, m.listFetch)
}

func (m *workflowsModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			case "enter", "y":
				id := m.selectedWorkflowID()
				draft := *m.pendingDraft
				m.pendingDraft = nil
				m.statusMessage = "Submitting rollback..."
				return m, asyncWorkflowRollback(m.ctx, m.rollbackFetch, id, draft)
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
			m.scrollOffset = 0
			return m, asyncWorkflowListLoad(m.ctx, m.listFetch)
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
		case "b":
			if m.selectedWorkflowID() != "" {
				draft := newActionDraft("rollback")
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
			m.showPolicy = true
			m.showTrace = true
			m.showAudit = true
			m.scrollOffset = 0
			return m, m.loadLinkedContext(true)
		case "p":
			if policyID := m.selectedPolicyID(); policyID != "" {
				m.showPolicy = !m.showPolicy
				if m.showPolicy && m.linkedPolicyID != policyID {
					return m, asyncPolicyDetailLoad(m.ctx, m.policyFetch, policyID)
				}
			}
		case "t":
			m.showTrace = !m.showTrace
			if m.showTrace {
				if policyID := m.selectedPolicyID(); policyID != "" && policyID != m.linkedPolicyID {
					return m, asyncPolicyDetailLoad(m.ctx, m.policyFetch, policyID)
				}
			}
		case "u":
			m.showAudit = !m.showAudit
			if m.showAudit && m.auditFetch != nil && m.selectedWorkflowID() != "" {
				return m, asyncAuditLoad(m.ctx, func(ctx context.Context) (auditListResult, error) {
					return m.auditFetch(ctx, m.selectedWorkflowID())
				})
			}
		}
	case workflowListLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.rows = msg.data.Rows
			if m.initialID != "" {
				for i, row := range m.rows {
					if row.WorkflowID == m.initialID {
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
	case workflowDetailLoadedMsg:
		if msg.err == nil {
			m.detail = msg.data
			if m.showPolicy {
				if policyID := m.selectedPolicyID(); policyID != "" && policyID != m.linkedPolicyID {
					return m, asyncPolicyDetailLoad(m.ctx, m.policyFetch, policyID)
				}
			}
		} else {
			m.err = msg.err
		}
		if !m.smokeDone && m.smokeAction == "rollback" && m.selectedWorkflowID() != "" {
			m.smokeDone = true
			m.statusMessage = "Submitting rollback..."
			return m, asyncWorkflowRollback(m.ctx, m.rollbackFetch, m.selectedWorkflowID(), newActionDraft("rollback"))
		}
	case workflowRollbackLoadedMsg:
		if msg.err != nil {
			m.err = msg.err
			m.statusMessage = "Rollback failed"
			if m.autoExit {
				return m, tea.Quit
			}
			return m, nil
		}
		m.statusMessage = fmt.Sprintf("Rollback submitted: %s", msg.data.ActionID)
		if m.autoExit {
			return m, tea.Quit
		}
		return m, asyncWorkflowListLoad(m.ctx, m.listFetch)
	case policyDetailLoadedMsg:
		if msg.err == nil {
			m.linkedPolicy = msg.data
			m.linkedPolicyID = msg.data.Summary.PolicyID
		} else {
			m.err = msg.err
		}
	case auditLoadedMsg:
		if msg.err == nil {
			m.linkedAudit = msg.data
		} else {
			m.err = msg.err
		}
	}
	return m, nil
}

func (m *workflowsModel) loadLinkedContext(openAll bool) tea.Cmd {
	cmds := []tea.Cmd{}
	if policyID := m.selectedPolicyID(); policyID != "" {
		if openAll {
			m.showPolicy = true
			m.showTrace = true
		}
		if (m.showPolicy || m.showTrace) && policyID != m.linkedPolicyID {
			cmds = append(cmds, asyncPolicyDetailLoad(m.ctx, m.policyFetch, policyID))
		}
	}
	if m.auditFetch != nil && m.selectedWorkflowID() != "" {
		if openAll {
			m.showAudit = true
		}
		if m.showAudit {
			cmds = append(cmds, asyncAuditLoad(m.ctx, func(ctx context.Context) (auditListResult, error) {
				return m.auditFetch(ctx, m.selectedWorkflowID())
			}))
		}
	}
	return tea.Batch(cmds...)
}

func (m *workflowsModel) selectedWorkflowID() string {
	if len(m.rows) == 0 || m.selected < 0 || m.selected >= len(m.rows) {
		return ""
	}
	return m.rows[m.selected].WorkflowID
}

func (m *workflowsModel) loadSelection() tea.Cmd {
	id := m.selectedWorkflowID()
	if id == "" {
		return nil
	}
	return asyncWorkflowDetailLoad(m.ctx, m.detailFetch, id)
}

func (m *workflowsModel) selectedPolicyID() string {
	if len(m.detail.Policies) > 0 {
		return m.detail.Policies[0].PolicyID
	}
	return strings.TrimSpace(m.detail.Summary.LatestPolicyID)
}

func (m *workflowsModel) View() string {
	compact := compactLayout(m.width)
	tiny := tinyLayout(m.width)
	header := []string{
		titleStyle.Render("CyberMesh Workflows"),
		helpStyle.Render(workflowHelpText(compact, m.detailMode)),
		"",
		kvLine("View", m.title),
		kvLine("Selected", m.selectedWorkflowID()),
	}
	if m.statusMessage != "" {
		header = append(header, helpStyle.Render(m.statusMessage))
	}
	if m.loading {
		return joinLines(append(header, "", "Loading workflows...")...)
	}
	if m.showHelp {
		return renderHelpOverlay("Workflows Help", []string{
			"j/k or arrows: move selection",
			"enter: open or close expanded detail",
			"o: open linked policy, trace, and audit context",
			"p: toggle linked policy summary",
			"t: toggle linked trace pane",
			"u: toggle recent audit pane",
			"b: rollback selected workflow",
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
		body = append(body, kvLine("Workflow", m.selectedWorkflowID()))
		body = append(body, kvLine("Latest Policy", m.detail.Summary.LatestPolicyID))
		body = append(body, kvLine("Latest Status", renderStatus(m.detail.Summary.LatestStatus)))
		body = append(body, kvLine("Latest Trace", m.detail.Summary.LatestTraceID))
		body = append(body, kvLine("Request", m.detail.Summary.RequestID))
		body = append(body, kvLine("Command", m.detail.Summary.CommandID))
		body = append(body, kvLine("Latest Ack Result", renderStatus(m.detail.Summary.LatestAckResult)))
		body = append(body, kvLine("Policy Count", fmt.Sprintf("%d", len(m.detail.Policies))))
		body = append(body, kvLine("Outbox Rows", fmt.Sprintf("%d", len(m.detail.Outbox))))
		body = append(body, kvLine("ACK Rows", fmt.Sprintf("%d", len(m.detail.Acks))))
		body = append(body, kvLine("Policy States", workflowPolicyBreakdown(m.detail.Policies)))
		if len(m.detail.Policies) > 0 {
			body = append(body, "")
			body = append(body, labelStyle.Render("Member Policies"))
			for _, row := range m.detail.Policies[:minInt(len(m.detail.Policies), 5)] {
				body = append(body, fmt.Sprintf("%s  %s", shortID(row.PolicyID, 18), renderStatus(row.LatestStatus)))
			}
		}
		if m.linkedPolicyID != "" {
			body = append(body, "")
			body = append(body, labelStyle.Render("Linked Policy"))
			body = append(body, kvLine("Policy", m.linkedPolicy.Summary.PolicyID))
			body = append(body, kvLine("Status", renderStatus(m.linkedPolicy.Summary.LatestStatus)))
			body = append(body, kvLine("Trace", m.linkedPolicy.Summary.TraceID))
			body = append(body, kvLine("Workflow", m.linkedPolicy.Summary.WorkflowID))
			body = append(body, kvLine("Ack Result", renderStatus(m.linkedPolicy.Summary.LatestAckResult)))
		}
		if m.showTrace && m.linkedPolicyID != "" {
			body = append(body, "")
			body = append(body, labelStyle.Render("Linked Trace"))
			body = append(body, kvLine("Trace Policy", m.linkedPolicy.Trace.PolicyID))
			body = append(body, kvLine("Trace ID", m.linkedPolicy.Trace.TraceID))
			body = append(body, kvLine("Source Event", m.linkedPolicy.Trace.SourceEventID))
			body = append(body, kvLine("Sentinel Event", m.linkedPolicy.Trace.SentinelEventID))
			body = append(body, kvLine("Trace Outbox", fmt.Sprintf("%d", len(m.linkedPolicy.Trace.Outbox))))
			body = append(body, kvLine("Trace ACKs", fmt.Sprintf("%d", len(m.linkedPolicy.Trace.Acks))))
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
	body = append(body, renderWorkflowListPane(m.rows, m.selected, listRowsForHeight(m.height), compact)...)
	body = append(body, "")
	body = append(body, labelStyle.Render("Workflow Details"))
	body = append(body, kvLine("Latest Policy", m.detail.Summary.LatestPolicyID))
	body = append(body, kvLine("Latest Status", renderStatus(m.detail.Summary.LatestStatus)))
	body = append(body, kvLine("Latest Trace", m.detail.Summary.LatestTraceID))
	body = append(body, kvLine("Latest Ack Result", renderStatus(m.detail.Summary.LatestAckResult)))
	body = append(body, kvLine("Outbox Rows", fmt.Sprintf("%d", m.detail.Summary.OutboxCount)))
	body = append(body, kvLine("Policies", fmt.Sprintf("%d", len(m.detail.Policies))))
	body = append(body, kvLine("ACK Rows", fmt.Sprintf("%d", len(m.detail.Acks))))
	if !compact {
		body = append(body, kvLine("Request", m.detail.Summary.RequestID))
		body = append(body, kvLine("Command", m.detail.Summary.CommandID))
		body = append(body, kvLine("Policy States", workflowPolicyBreakdown(m.detail.Policies)))
	}
	if m.showPolicy && m.linkedPolicyID != "" && !tiny {
		body = append(body, "")
		body = append(body, labelStyle.Render("Linked Policy"))
		body = append(body, kvLine("Policy", m.linkedPolicy.Summary.PolicyID))
		body = append(body, kvLine("Status", renderStatus(m.linkedPolicy.Summary.LatestStatus)))
		body = append(body, kvLine("Trace", m.linkedPolicy.Summary.TraceID))
		body = append(body, kvLine("Workflow", m.linkedPolicy.Summary.WorkflowID))
		body = append(body, kvLine("Ack Result", renderStatus(m.linkedPolicy.Summary.LatestAckResult)))
	}
	if m.showTrace && m.linkedPolicyID != "" && !tiny {
		body = append(body, "")
		body = append(body, labelStyle.Render("Linked Trace"))
		body = append(body, kvLine("Trace Policy", m.linkedPolicy.Trace.PolicyID))
		body = append(body, kvLine("Trace ID", m.linkedPolicy.Trace.TraceID))
		body = append(body, kvLine("Source Event", m.linkedPolicy.Trace.SourceEventID))
		body = append(body, kvLine("Sentinel Event", m.linkedPolicy.Trace.SentinelEventID))
		body = append(body, kvLine("Trace Outbox", fmt.Sprintf("%d", len(m.linkedPolicy.Trace.Outbox))))
		body = append(body, kvLine("Trace ACKs", fmt.Sprintf("%d", len(m.linkedPolicy.Trace.Acks))))
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
		body = append(body, errorStyle.Render("Confirm ROLLBACK"))
		body = append(body, fmt.Sprintf("Workflow %s", blankDash(m.selectedWorkflowID())))
		body = append(body, kvLine("Current Status", renderStatus(m.detail.Summary.LatestStatus)))
		body = append(body, kvLine("Policies", fmt.Sprintf("%d", len(m.detail.Policies))))
		body = append(body, kvLine("Latest Policy", m.detail.Summary.LatestPolicyID))
		body = append(body, kvLine("Latest Ack", renderStatus(m.detail.Summary.LatestAckResult)))
		body = append(body, kvLine(fieldCursor("reason_code", m.pendingDraft.Focus == 0), m.pendingDraft.ReasonCode))
		body = append(body, kvLine(fieldCursor("reason_text", m.pendingDraft.Focus == 1), m.pendingDraft.ReasonText))
		body = append(body, kvLine(fieldCursor("classification", m.pendingDraft.Focus == 2), m.pendingDraft.Classification))
		body = append(body, helpStyle.Render("tab switch field • type to edit • enter/y confirm • esc/n cancel"))
	}
	return finalizeView(header, body, m.height, m.scrollOffset)
}

func workflowHelpText(compact bool, detailMode bool) string {
	if detailMode {
		if compact {
			return "j/k move • ? help • enter close • pg scroll • o linked • t/u panes • b rollback • q quit"
		}
		return "j/k move • ? help • enter close detail • pgup/pgdn scroll • o open linked • p policy • t trace • u audit • b rollback • q quit"
	}
	if compact {
		return "j/k move • ? help • enter detail • b rollback • p/t/u/o linked • q quit"
	}
	return "j/k move • ? help • enter detail • r refresh • b rollback • p policy • t trace • u audit • o open linked • q quit"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func workflowPolicyBreakdown(rows []policySummary) string {
	if len(rows) == 0 {
		return "-"
	}
	counts := map[string]int{}
	for _, row := range rows {
		key := strings.ToLower(strings.TrimSpace(row.LatestStatus))
		if key == "" {
			key = "unknown"
		}
		counts[key]++
	}
	parts := make([]string, 0, len(counts))
	for _, key := range []string{"pending", "publishing", "published", "acked", "retry", "terminal_failed", "unknown"} {
		if counts[key] > 0 {
			parts = append(parts, fmt.Sprintf("%s=%d", key, counts[key]))
		}
	}
	return strings.Join(parts, ", ")
}

func renderWorkflowListPane(rows []workflowSummary, selected int, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render("Workflows")}
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
			lines = append(lines, fmt.Sprintf("%s %s  %s", prefix, shortID(row.WorkflowID, 16), renderStatus(row.LatestStatus)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  %d", prefix, shortID(row.WorkflowID, 18), renderStatus(row.LatestStatus), row.PolicyCount))
	}
	if end < len(rows) {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more rows ...", len(rows)-end)))
	}
	return lines
}
