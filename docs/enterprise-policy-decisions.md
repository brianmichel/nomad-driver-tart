# Approved enterprise policy decisions

These are product decisions, not claims that the implementations or qualification
are complete.

- **R06 / #52:** Preserve trusted single-tenant behavior by default. Restricted
  job-author mode is opt-in and must enforce operator-controlled mount/write and
  network/interface allowlists before VM creation.
- **R08 / #54:** Require pinned SSH server host keys in restricted mode. Preserve
  clearly documented trusted-mode compatibility. No implicit trust-on-first-use
  bootstrap was approved.
- **R10 / #56 and R15 / #60:** Prepare qualification tooling; keep actual Apple
  Silicon/Tart/Nomad results explicitly pending. No test host or live host/VM
  changes are authorized by this decision.
- **CI publication:** Leave publication blocked for now. Keep the reviewed
  `enterprise/compatibility-gates` commit local. Do not modify credentials,
  bypass the workflow-scope denial, or silently drop the active workflow to
  publish a different PR.

Nomad ACLs, host OS permissions and explicit host-operation authorization still
apply. Restricted configuration alone is not hostile-tenant isolation proof.
