# SPIFFE Trust Domain

Status: Approved for Phase 0
Owner: Platform Security
Last Updated: 2026-04-07
Source of truth: `security/docs/workload_authorization_matrix.md`

## 1. Trust Domain
Phase 0 freezes the initial trust domain string as:

`cybermesh.internal`

## 2. Rationale
1. The trust domain is environment-neutral enough for platform design.
2. It avoids cloud-provider coupling in the identity namespace.
3. It matches the initial workload authorization matrix examples.

## 3. Rules
1. All protected workload identities must be issued under the same trust domain unless a reviewed exception is documented.
2. Trust domain changes require coordinated updates to:
   - SPIFFE ID catalog
   - workload authorization matrix
   - service auth configuration
   - observability and audit parsing rules
3. Trust domain changes are security-significant and require review before rollout.
