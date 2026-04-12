package api

import (
	"context"
	"fmt"
	"strings"
)

func appendAccessBoundAuditFilter(where []string, args []interface{}, tenantScope string) ([]string, []interface{}) {
	if strings.TrimSpace(tenantScope) == "" {
		return where, args
	}
	where = append(where, fmt.Sprintf("caj.tenant_scope = $%d", len(args)+1))
	args = append(args, tenantScope)
	return where, args
}

func appendAccessBoundOutboxFilter(where []string, args []interface{}, tenantScope string) ([]string, []interface{}) {
	if strings.TrimSpace(tenantScope) == "" {
		return where, args
	}
	tenantArg := len(args) + 1
	where = append(where, fmt.Sprintf("%s = $%d", outboxPayloadStringExpr("{tenant}", "{metadata,tenant}", "{trace,tenant}", "{target,tenant}", "{target,tenant_id}"), tenantArg))
	args = append(args, tenantScope)
	return where, args
}

func appendAccessBoundOutboxClause(base string, args []interface{}, tenantScope string) (string, []interface{}) {
	parts := []string{base}
	parts, args = appendAccessBoundOutboxFilter(parts, args, tenantScope)
	return strings.Join(parts, " AND "), args
}

func appendAccessBoundAckFilter(base string, args []interface{}, tenantScope string) (string, []interface{}) {
	if strings.TrimSpace(tenantScope) == "" {
		return base, args
	}
	base += fmt.Sprintf(" AND tenant = $%d", len(args)+1)
	args = append(args, tenantScope)
	return base, args
}

func accessBoundAckWhereClause(argIndex int, tenantScope string) string {
	if strings.TrimSpace(tenantScope) == "" {
		return ""
	}
	return fmt.Sprintf(" AND tenant = $%d", argIndex)
}

func (s *Server) outboxVisibleToAccess(_ context.Context, _ rowQueryer, row controlOutboxRow, tenantScope string) (bool, error) {
	if strings.TrimSpace(tenantScope) == "" {
		return true, nil
	}
	return tenantScopeMatchesOutboxRow(row, tenantScope), nil
}
