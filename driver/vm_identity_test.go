package driver

import (
	"context"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/nomad/plugins/drivers"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type identityClient struct {
	testClient
	stopped []string
	deleted []string
}

func (c *identityClient) Stop(_ context.Context, name string, _ time.Duration) error {
	c.stopped = append(c.stopped, name)
	return nil
}
func (c *identityClient) Delete(_ context.Context, name string) error {
	c.deleted = append(c.deleted, name)
	return nil
}
func identityHandle(t *testing.T, cfg *drivers.TaskConfig, state drivers.TaskState) *taskHandle {
	t.Helper()
	owner, err := newVMOwnership(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	close(done)
	return &taskHandle{ownership: owner, taskConfig: cfg, state: state, exec: &stubExecutor{}, doneCh: done, logger: hclog.NewNullLogger()}
}
func TestVMOwnershipStopAndDestroyCannotTouchSiblingTask(t *testing.T) {
	drv := NewTartDriver(hclog.NewNullLogger()).(*Driver)
	defer drv.signalShutdown()
	client := &identityClient{}
	drv.client = client
	a := &drivers.TaskConfig{ID: "task-a", Name: "a", AllocID: "same-alloc"}
	b := &drivers.TaskConfig{ID: "task-b", Name: "b", AllocID: "same-alloc"}
	drv.tasks.Set(a.ID, identityHandle(t, a, drivers.TaskStateRunning))
	drv.tasks.Set(b.ID, identityHandle(t, b, drivers.TaskStateExited))
	if err := drv.StopTask(a.ID, time.Second, ""); err != nil {
		t.Fatal(err)
	}
	if len(client.stopped) != 1 || client.stopped[0] != taskVMName(a) || len(client.deleted) != 1 || client.deleted[0] != taskVMName(a) {
		t.Fatalf("wrong stop/delete target: %#v / %#v", client.stopped, client.deleted)
	}
	if _, ok := drv.tasks.Get(b.ID); !ok {
		t.Fatal("sibling task disappeared")
	}
	if err := drv.DestroyTask(b.ID, false); err != nil {
		t.Fatal(err)
	}
	if client.deleted[len(client.deleted)-1] != taskVMName(b) {
		t.Fatalf("destroy targeted sibling: %#v", client.deleted)
	}
}
func TestVMOwnershipRejectsMutationBeforeDestructiveCalls(t *testing.T) {
	drv := NewTartDriver(hclog.NewNullLogger()).(*Driver)
	defer drv.signalShutdown()
	client := &identityClient{}
	drv.client = client
	cfg := &drivers.TaskConfig{ID: "task-a", Name: "a", AllocID: "original-alloc"}
	h := identityHandle(t, cfg, drivers.TaskStateExited)
	drv.tasks.Set(cfg.ID, h)
	cfg.AllocID = "foreign-alloc"
	if err := drv.StopTask(cfg.ID, time.Second, ""); err == nil {
		t.Fatal("stop accepted mismatched ownership")
	}
	if err := drv.DestroyTask(cfg.ID, true); err == nil {
		t.Fatal("destroy accepted mismatched ownership")
	}
	if len(client.stopped) > 0 || len(client.deleted) > 0 || h.exec.(*stubExecutor).shutdownCalled {
		t.Fatal("performed destructive work with mismatched ownership")
	}
}

func TestVMOwnershipMissingConfigRejectsStop(t *testing.T) {
	drv := NewTartDriver(hclog.NewNullLogger()).(*Driver)
	defer drv.signalShutdown()
	client := &identityClient{}
	drv.client = client
	cfg := &drivers.TaskConfig{ID: "task-a", Name: "a", AllocID: "alloc-a"}
	h := identityHandle(t, cfg, drivers.TaskStateExited)
	h.taskConfig = nil
	drv.tasks.Set(cfg.ID, h)
	if err := drv.StopTask(cfg.ID, time.Second, ""); err == nil {
		t.Fatal("stop accepted missing ownership configuration")
	}
	if h.exec.(*stubExecutor).shutdownCalled || len(client.deleted) > 0 {
		t.Fatal("destructive work before ownership validation")
	}
}
func TestVMOwnershipMalformedRecoveryHandle(t *testing.T) {
	drv := NewTartDriver(hclog.NewNullLogger()).(*Driver)
	defer drv.signalShutdown()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("malformed recovery handle panicked: %v", r)
		}
	}()
	if err := drv.RecoverTask(drivers.NewTaskHandle(2)); err == nil {
		t.Fatal("accepted missing recovery configuration")
	}
}

func TestVMOwnershipReservationProtectsSharedLegacyVM(t *testing.T) {
	store := newTaskStore()
	a := &drivers.TaskConfig{ID: "task-a", Name: "a", AllocID: "legacy-allocation"}
	b := &drivers.TaskConfig{ID: "task-b", Name: "b", AllocID: "legacy-allocation"}
	first, _ := newVMOwnership(a, false)
	first.Legacy = true
	first.VMName = vmName(a.AllocID)
	second, _ := newVMOwnership(b, false)
	second.Legacy = true
	second.VMName = vmName(b.AllocID)
	claim := store.reserveOwnership(a.ID, first)
	if claim == nil {
		t.Fatal("initial ownership claim failed")
	}
	if store.reserveOwnership(b.ID, second) != nil {
		t.Fatal("two tasks claimed the same legacy VM")
	}
	if store.reserveOwnership(a.ID, first) != nil {
		t.Fatal("duplicate startup claimed the same task")
	}
	store.releaseReservation(a.ID, claim)
	if store.reserveOwnership(b.ID, second) == nil {
		t.Fatal("failed startup reservation was not released")
	}
}

func TestVMOwnershipStatusExposesExactOwnedName(t *testing.T) {
	cfg := &drivers.TaskConfig{ID: "task-a", Name: "a", AllocID: "alloc-a"}
	h := identityHandle(t, cfg, drivers.TaskStateExited)
	if got := h.TaskStatus().DriverAttributes["vm_name"]; got != h.ownership.VMName {
		t.Fatalf("missing reviewable VM identity: %q", got)
	}
}

func TestVMOwnershipReservationIsAtomic(t *testing.T) {
	store := newTaskStore()
	cfg := &drivers.TaskConfig{ID: "task-a", Name: "a", AllocID: "alloc-a"}
	owner, _ := newVMOwnership(cfg, false)
	var winners atomic.Int32
	var workers sync.WaitGroup
	for i := 0; i < 16; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if store.reserveOwnership(cfg.ID, owner) != nil {
				winners.Add(1)
			}
		}()
	}
	workers.Wait()
	if winners.Load() != 1 {
		t.Fatalf("concurrent startup owners: %d", winners.Load())
	}
}
func TestVMOwnershipStateRoundTrip(t *testing.T) {
	cfg := &drivers.TaskConfig{ID: "task-a", Name: "a", AllocID: "alloc-a"}
	owner, _ := newVMOwnership(cfg, false)
	handle := drivers.NewTaskHandle(2)
	handle.Config = cfg
	if err := handle.SetDriverState(&driverState{TaskConfig: cfg, VMOwnership: owner}); err != nil {
		t.Fatal(err)
	}
	var state driverState
	if err := handle.GetDriverState(&state); err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeVMOwnership(handle, state)
	if err != nil {
		t.Fatal(err)
	}
	if decoded != owner {
		t.Fatalf("persisted identity changed: %#v", decoded)
	}
	state.VMOwnership.VMName = "foreign-vm"
	if _, err := decodeVMOwnership(handle, state); err == nil {
		t.Fatal("recovered foreign VM")
	}
	state.VMOwnership = vmOwnership{}
	if _, err := decodeVMOwnership(handle, state); err == nil {
		t.Fatal("silently accepted missing v2 identity")
	}
	for _, version := range []int{0, 3} {
		handle.Version = version
		if _, err := decodeVMOwnership(handle, state); err == nil {
			t.Fatalf("accepted version %d", version)
		}
	}
}

func TestVMOwnershipPinsTaskAndAllocation(t *testing.T) {
	cfg := &drivers.TaskConfig{ID: "task-a", Name: "alpha", AllocID: "alloc-a"}
	owner, err := newVMOwnership(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if owner.VMName != taskVMName(cfg) || owner.TaskID != cfg.ID || owner.AllocID != cfg.AllocID {
		t.Fatalf("missing exact ownership: %#v", owner)
	}
	if err := owner.validate(cfg, "other-task", false); err == nil {
		t.Fatal("accepted wrong task")
	}
	changed := *cfg
	changed.AllocID = "other-allocation"
	if err := owner.validate(&changed, cfg.ID, false); err == nil {
		t.Fatal("accepted changed allocation")
	}
	foreign := owner
	foreign.VMName = "unrelated-user-vm"
	if err := foreign.validate(cfg, cfg.ID, false); err == nil {
		t.Fatal("accepted unrelated VM")
	}
}

func TestVMOwnershipLegacyHandleKeepsExactAllocationName(t *testing.T) {
	cfg := &drivers.TaskConfig{ID: "task-a", Name: "alpha", AllocID: "alloc-a"}
	handle := drivers.NewTaskHandle(1)
	handle.Config = cfg
	state := driverState{TaskConfig: cfg}
	owner, err := decodeVMOwnership(handle, state)
	if err != nil {
		t.Fatal(err)
	}
	if !owner.Legacy || owner.VMName != "nomad-alloc-a" {
		t.Fatalf("legacy VM renamed: %#v", owner)
	}
	if err := owner.validate(cfg, cfg.ID, false); err != nil {
		t.Fatal(err)
	}
	other := *cfg
	other.ID = "different-task"
	handle.Config = &other
	if _, err := decodeVMOwnership(handle, state); err == nil {
		t.Fatal("accepted mismatched outer handle ownership")
	}
}

func TestVMIdentityDistinctTasksInSameAllocation(t *testing.T) {
	client := NewTartClient(hclog.NewNullLogger())
	first := VMConfig{Nomad: &drivers.TaskConfig{ID: "alloc/task-a/one", Name: "task-a", AllocID: "same-allocation"}}
	second := VMConfig{Nomad: &drivers.TaskConfig{ID: "alloc/task-b/two", Name: "task-b", AllocID: "same-allocation"}}
	a, err := client.BuildStartArgs(first)
	if err != nil {
		t.Fatal(err)
	}
	b, err := client.BuildStartArgs(second)
	if err != nil {
		t.Fatal(err)
	}
	if a[1] == b[1] {
		t.Fatalf("two tasks in one allocation target the same VM: %q", a[1])
	}
	again, err := client.BuildStartArgs(first)
	if err != nil {
		t.Fatal(err)
	}
	if again[1] != a[1] {
		t.Fatalf("VM identity is not stable: %q vs %q", a[1], again[1])
	}
}
