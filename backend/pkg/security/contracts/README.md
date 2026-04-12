# Backend Local Contract Mirror

These files mirror the Phase 0 contract definitions in `security/contracts`.

Rules:

- Backend imports the local package `backend/pkg/security/contracts`.
- `security/contracts` remains the cross-service source of truth.
- Any contract change must update both locations in the same change.
- CI enforces exact file parity via `backend/scripts/check-contract-sync.ps1`.
