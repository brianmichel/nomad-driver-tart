package driver

import (
	"github.com/hashicorp/nomad/plugins/drivers"
	"testing"
)

// Deterministically reproduce Destroy/new-start between the first startup's
// handle publication and its deferred reservation cleanup (an ABA interleaving).
func TestVMOwnershipStaleReleaseCannotDropNewClaim(t *testing.T) {
	store := newTaskStore()
	cfg := &drivers.TaskConfig{ID: "task-a", Name: "a", AllocID: "alloc-a"}
	owner, err := newVMOwnership(cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	claim := store.reserveOwnership(cfg.ID, owner)
	if claim == nil {
		t.Fatal("initial reservation failed")
	}
	handle := &taskHandle{ownership: owner}
	store.Set(cfg.ID, handle)
	store.releaseReservation(cfg.ID, claim)
	if store.reserveOwnership(cfg.ID, owner) != nil {
		t.Fatal("deferred cleanup removed a live handle's claim")
	}
	store.Delete(cfg.ID, handle)
	replacement := store.reserveOwnership(cfg.ID, owner)
	if replacement == nil {
		t.Fatal("replacement reservation failed")
	}
	store.Delete(cfg.ID, handle)
	if store.reserveOwnership(cfg.ID, owner) != nil {
		t.Fatal("stale handle deletion removed replacement reservation")
	}
	// The first startup's defer now runs, after the replacement owns the ID.
	store.releaseReservation(cfg.ID, claim)
	if store.reserveOwnership(cfg.ID, owner) != nil {
		t.Fatal("stale deferred cleanup removed a replacement startup's reservation")
	}
	store.releaseReservation(cfg.ID, replacement)
	if store.reserveOwnership(cfg.ID, owner) == nil {
		t.Fatal("replacement startup could not release its own reservation")
	}
}
