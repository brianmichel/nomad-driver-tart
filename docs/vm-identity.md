# VM identity and compatibility (draft)

New VM tasks use a bounded `nomad-<SHA-256>` name derived unambiguously from the
allocation ID, task ID and task name. Two Tart tasks in one allocation therefore
have separate VM targets. The same task identity is stable across restarts.
`InspectTask` exposes the exact target in `DriverAttributes.vm_name` (empty for
pull-only tasks). Do not infer VM ownership from the `nomad-` prefix alone.

Handle version 2 persists the exact VM name and owning allocation/task metadata.
Start, stop, destroy, IP lookup and SSH exec use that identity. Destructive VM
operations reject missing or conflicting ownership before shutting down the
executor or touching a VM. In-process atomic reservations reject duplicate
startup and prevent different legacy tasks claiming the same allocation-named
VM. Reservations are not host-level capacity admission or a distributed lock.

Version 1 handles retain their exact `nomad-<allocation-id>` identity. They are
not renamed and unrelated VMs are not searched for or adopted. A recovered legacy
identity can be persisted in version 2 with an explicit legacy marker. Version 2
handles with missing or mismatched ownership, unknown versions, and conflicting
outer/inner task identities fail closed. Rollback to a version-1-only driver is
not supported for version 2 handles; drain tasks before downgrading.

## Verification boundaries

Portable tests cover separate targets for sibling tasks, stable names, state
serialization, legacy mapping, ownership mismatch rejection, stop/destroy
isolation, and concurrent reservations. Tart execution is mocked in these tests;
this is **not** an Apple Silicon two-VM/restart qualification result.

Recovery still follows the pre-existing start/replay path in this draft. The
separate R03/#49 change must replace that path with executor/VM reattachment
before restart safety is qualified. R04/#50 separately handles transactional
rollback and retryable cleanup errors. Neither prerequisite is claimed solved
by naming changes.
