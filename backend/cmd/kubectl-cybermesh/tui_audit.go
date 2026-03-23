package main

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

type auditModel struct {
	ctx            context.Context
	title          string
	fetch          auditFetcher
	policyFetch    policyDetailFetcher
	workflowFetch  workflowDetailFetcher
	autoExit       bool
	loading        bool
	rows           []auditEntry
	selected       int
	err            error
	showPolicy     bool
	showWorkflow   bool
	showTrace      bool
	linkedPolicy   policyDetailResult
	linkedPolicyID string
	linkedWork     workflowDetailResult
	linkedWorkID   string
	detailMode     bool
	showHelp       bool
	scrollOffset   int
	width          int
	height         int
}

func newAuditModel(ctx context.Context, title string, fetch auditFetcher, policyFetch policyDetailFetcher, workflowFetch workflowDetailFetcher) tea.Model {
	width, height := initialTerminalSize()
	return &auditModel{
		ctx:           ctx,
		title:         title,
		fetch:         fetch,
		policyFetch:   policyFetch,
		workflowFetch: workflowFetch,
		autoExit:      tuiAutoExitEnabled(),
		loading:       true,
		width:         width,
		height:        height,
	}
}

func (m *auditModel) Init() tea.Cmd { return asyncAuditLoad(m.ctx, m.fetch) }

func (m *auditModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			return m, asyncAuditLoad(m.ctx, m.fetch)
		case "up", "k":
			if m.selected > 0 {
				m.selected--
				m.scrollOffset = 0
			}
		case "down", "j":
			if m.selected < len(m.rows)-1 {
				m.selected++
				m.scrollOffset = 0
			}
		case "enter":
			m.detailMode = !m.detailMode
			m.scrollOffset = 0
			cmds := []tea.Cmd{}
			if m.detailMode {
				if policyID := m.selectedPolicyID(); policyID != "" && policyID != m.linkedPolicyID {
					m.showPolicy = true
					cmds = append(cmds, asyncPolicyDetailLoad(m.ctx, m.policyFetch, policyID))
				}
				if workflowID := m.selectedWorkflowID(); workflowID != "" && workflowID != m.linkedWorkID {
					m.showWorkflow = true
					cmds = append(cmds, asyncWorkflowDetailLoad(m.ctx, m.workflowFetch, workflowID))
				}
			}
			return m, tea.Batch(cmds...)
		case "o":
			m.detailMode = true
			m.showPolicy = true
			m.showWorkflow = true
			m.showTrace = true
			m.scrollOffset = 0
			cmds := []tea.Cmd{}
			if policyID := m.selectedPolicyID(); policyID != "" && policyID != m.linkedPolicyID {
				m.showPolicy = true
				cmds = append(cmds, asyncPolicyDetailLoad(m.ctx, m.policyFetch, policyID))
			}
			if workflowID := m.selectedWorkflowID(); workflowID != "" && workflowID != m.linkedWorkID {
				m.showWorkflow = true
				cmds = append(cmds, asyncWorkflowDetailLoad(m.ctx, m.workflowFetch, workflowID))
			}
			return m, tea.Batch(cmds...)
		case "p":
			if policyID := m.selectedPolicyID(); policyID != "" {
				m.showPolicy = !m.showPolicy
				if m.showPolicy && m.linkedPolicyID != policyID {
					return m, asyncPolicyDetailLoad(m.ctx, m.policyFetch, policyID)
				}
			}
		case "w":
			if workflowID := m.selectedWorkflowID(); workflowID != "" {
				m.showWorkflow = !m.showWorkflow
				if m.showWorkflow && m.linkedWorkID != workflowID {
					return m, asyncWorkflowDetailLoad(m.ctx, m.workflowFetch, workflowID)
				}
			}
		case "t":
			m.showTrace = !m.showTrace
			if m.showTrace {
				if policyID := m.selectedPolicyID(); policyID != "" && policyID != m.linkedPolicyID {
					return m, asyncPolicyDetailLoad(m.ctx, m.policyFetch, policyID)
				}
			}
		}
	case auditLoadedMsg:
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.rows = msg.data.Rows
			if m.selected >= len(m.rows) {
				m.selected = maxInt(0, len(m.rows)-1)
			}
		}
		if m.autoExit {
			return m, tea.Quit
		}
	case policyDetailLoadedMsg:
		if msg.err == nil {
			m.linkedPolicy = msg.data
			m.linkedPolicyID = msg.data.Summary.PolicyID
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
	}
	return m, nil
}

func (m *auditModel) View() string {
	compact := compactLayout(m.width)
	tiny := tinyLayout(m.width)
	header := []string{
		titleStyle.Render("CyberMesh Audit"),
		helpStyle.Render(auditHelpText(compact, m.detailMode)),
		"",
		kvLine("View", m.title),
	}
	if m.loading {
		return joinLines(append(header, "", "Loading audit rows...")...)
	}
	if m.showHelp {
		return renderHelpOverlay("Audit Help", []string{
			"j/k or arrows: move selection",
			"enter: open or close expanded detail",
			"o: open linked policy, workflow, and trace context",
			"p: toggle linked policy summary",
			"w: toggle linked workflow summary",
			"t: toggle linked trace pane",
			"PgUp / PgDn: scroll the full detail body",
			"Ctrl+U / Ctrl+D: faster full-body scroll",
			"r: refresh",
			"q: quit",
		}, m.height)
	}
	if m.err != nil {
		header = append(header, "", errorStyle.Render(m.err.Error()))
		return joinLines(header...)
	}
	body := []string{}
	if m.detailMode {
		body = append(body, "")
		body = append(body, labelStyle.Render("Expanded Detail"))
		body = append(body, m.selectedDetails(false)...)
		if m.linkedPolicyID != "" {
			body = append(body, "")
			body = append(body, labelStyle.Render("Linked Policy"))
			body = append(body, kvLine("Policy", m.linkedPolicy.Summary.PolicyID))
			body = append(body, kvLine("Status", renderStatus(m.linkedPolicy.Summary.LatestStatus)))
			body = append(body, kvLine("Workflow", m.linkedPolicy.Summary.WorkflowID))
			body = append(body, kvLine("Trace", m.linkedPolicy.Summary.TraceID))
			body = append(body, kvLine("Ack Result", renderStatus(m.linkedPolicy.Summary.LatestAckResult)))
		}
		if m.linkedWorkID != "" {
			body = append(body, "")
			body = append(body, labelStyle.Render("Linked Workflow"))
			body = append(body, kvLine("Workflow", m.linkedWork.Summary.WorkflowID))
			body = append(body, kvLine("Latest Policy", m.linkedWork.Summary.LatestPolicyID))
			body = append(body, kvLine("Latest Status", renderStatus(m.linkedWork.Summary.LatestStatus)))
			body = append(body, kvLine("Latest Trace", m.linkedWork.Summary.LatestTraceID))
			body = append(body, kvLine("Policies", fmt.Sprintf("%d", len(m.linkedWork.Policies))))
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
		return finalizeView(header, body, m.height, m.scrollOffset)
	}
	body = append(body, "")
	body = append(body, renderAuditPane(m.rows, m.selected, listRowsForHeight(m.height), compact)...)
	body = append(body, "")
	body = append(body, labelStyle.Render("Selected Entry"))
	body = append(body, m.selectedDetails(compact)...)
	if m.showPolicy && m.linkedPolicyID != "" && !tiny {
		body = append(body, "")
		body = append(body, labelStyle.Render("Linked Policy"))
		body = append(body, kvLine("Policy", m.linkedPolicy.Summary.PolicyID))
		body = append(body, kvLine("Status", renderStatus(m.linkedPolicy.Summary.LatestStatus)))
		body = append(body, kvLine("Workflow", m.linkedPolicy.Summary.WorkflowID))
		body = append(body, kvLine("Trace", m.linkedPolicy.Summary.TraceID))
	}
	if m.showWorkflow && m.linkedWorkID != "" && !tiny {
		body = append(body, "")
		body = append(body, labelStyle.Render("Linked Workflow"))
		body = append(body, kvLine("Workflow", m.linkedWork.Summary.WorkflowID))
		body = append(body, kvLine("Latest Policy", m.linkedWork.Summary.LatestPolicyID))
		body = append(body, kvLine("Latest Status", renderStatus(m.linkedWork.Summary.LatestStatus)))
		body = append(body, kvLine("Policies", fmt.Sprintf("%d", len(m.linkedWork.Policies))))
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
	return finalizeView(header, body, m.height, m.scrollOffset)
}

func auditHelpText(compact bool, detailMode bool) string {
	if detailMode {
		if compact {
			return "j/k move • ? help • enter close • pg scroll • o linked • p/w/t panes • q quit"
		}
		return "j/k move • ? help • enter close detail • pgup/pgdn scroll • o open linked • p policy • w workflow • t trace • q quit"
	}
	if compact {
		return "j/k move • ? help • enter detail • p/w/t/o linked • q quit"
	}
	return "j/k move • ? help • enter detail • r refresh • p policy • w workflow • t trace • o open linked • q quit"
}

func (m *auditModel) selectedPolicyID() string {
	if len(m.rows) == 0 {
		return ""
	}
	return m.rows[m.selected].PolicyID
}

func (m *auditModel) selectedWorkflowID() string {
	if len(m.rows) == 0 {
		return ""
	}
	return m.rows[m.selected].WorkflowID
}

func (m *auditModel) selectedDetails(compact bool) []string {
	if len(m.rows) == 0 {
		return []string{"-"}
	}
	row := m.rows[m.selected]
	lines := []string{
		kvLine("Action", row.ActionType),
		kvLine("Target", row.TargetKind),
		kvLine("Policy", row.PolicyID),
		kvLine("Workflow", row.WorkflowID),
		kvLine("Actor", row.Actor),
		kvLine("Reason Code", row.ReasonCode),
		kvLine("When", formatTimestampAuto(row.CreatedAt)),
	}
	if !compact {
		lines = append(lines,
			kvLine("Request", row.RequestID),
			kvLine("Idempotency", row.IdempotencyKey),
			kvLine("Before", row.BeforeStatus),
			kvLine("After", row.AfterStatus),
			kvLine("Tenant", row.TenantScope),
			kvLine("Class", row.Classification),
		)
	}
	return lines
}

func renderAuditPane(rows []auditEntry, selected int, maxRows int, compact bool) []string {
	lines := []string{labelStyle.Render("Audit Rows")}
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
			lines = append(lines, fmt.Sprintf("%s %s  %s", prefix, shortID(row.ActionID, 14), renderStatus(row.ActionType)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s  %s  %s", prefix, shortID(row.ActionID, 18), renderStatus(row.ActionType), blankDash(shortID(row.WorkflowID, 18))))
	}
	if end < len(rows) {
		lines = append(lines, helpStyle.Render(fmt.Sprintf("... %d more rows ...", len(rows)-end)))
	}
	return lines
}
