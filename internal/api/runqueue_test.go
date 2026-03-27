package api

import (
	"testing"
	"time"
)

func TestAddTransitionsIdleToReady(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()
	if rq.GetState().State != QueueStateIdle {
		t.Fatalf("initial state = %q, want %q", rq.GetState().State, QueueStateIdle)
	}

	rq.Add(QueueItem{WorkOrderFile: "wo1.yaml", Title: "First"})

	snap := rq.GetState()
	if snap.State != QueueStateReady {
		t.Fatalf("state after add = %q, want %q", snap.State, QueueStateReady)
	}
	if len(snap.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(snap.Items))
	}
	if snap.Items[0].Status != QueueItemPending {
		t.Fatalf("item status = %q, want %q", snap.Items[0].Status, QueueItemPending)
	}
	if snap.Items[0].Title != "First" {
		t.Fatalf("item title = %q, want %q", snap.Items[0].Title, "First")
	}
}

func TestRemovePendingItem(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()
	item := rq.Add(QueueItem{WorkOrderFile: "wo1.yaml", Title: "First"})
	rq.Add(QueueItem{WorkOrderFile: "wo2.yaml", Title: "Second"})

	if err := rq.Remove(item.ID); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	snap := rq.GetState()
	if len(snap.Items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(snap.Items))
	}
	if snap.Items[0].Title != "Second" {
		t.Fatalf("remaining item title = %q, want %q", snap.Items[0].Title, "Second")
	}
}

func TestRemoveLastItemGoesIdle(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()
	item := rq.Add(QueueItem{WorkOrderFile: "wo1.yaml", Title: "Only"})

	if err := rq.Remove(item.ID); err != nil {
		t.Fatalf("Remove() error: %v", err)
	}

	snap := rq.GetState()
	if snap.State != QueueStateIdle {
		t.Fatalf("state after removing last = %q, want %q", snap.State, QueueStateIdle)
	}
	if len(snap.Items) != 0 {
		t.Fatalf("len(items) = %d, want 0", len(snap.Items))
	}
}

func TestRemoveExecutingReturnsError(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()

	// Block execution so we can test removing an executing item.
	started := make(chan struct{})
	block := make(chan struct{})
	rq.SetCallbacks(
		func(item QueueItem) (string, error) {
			close(started)
			<-block
			return "wf-1", nil
		},
		func(workflowID string) (string, error) {
			return "completed", nil
		},
	)

	item := rq.Add(QueueItem{WorkOrderFile: "wo1.yaml", Title: "Blocked"})
	if err := rq.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	<-started

	err := rq.Remove(item.ID)
	close(block)

	if err != ErrItemExecuting {
		t.Fatalf("Remove() error = %v, want ErrItemExecuting", err)
	}
}

func TestReorderWorks(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()
	a := rq.Add(QueueItem{WorkOrderFile: "a.yaml", Title: "A"})
	b := rq.Add(QueueItem{WorkOrderFile: "b.yaml", Title: "B"})
	c := rq.Add(QueueItem{WorkOrderFile: "c.yaml", Title: "C"})

	if err := rq.Reorder([]string{c.ID, a.ID, b.ID}); err != nil {
		t.Fatalf("Reorder() error: %v", err)
	}

	snap := rq.GetState()
	if len(snap.Items) != 3 {
		t.Fatalf("len(items) = %d, want 3", len(snap.Items))
	}
	if snap.Items[0].ID != c.ID || snap.Items[1].ID != a.ID || snap.Items[2].ID != b.ID {
		t.Fatalf("order = [%s, %s, %s], want [%s, %s, %s]",
			snap.Items[0].ID, snap.Items[1].ID, snap.Items[2].ID,
			c.ID, a.ID, b.ID)
	}
}

func TestReorderInvalidIDsReturnsError(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()
	rq.Add(QueueItem{WorkOrderFile: "a.yaml", Title: "A"})

	err := rq.Reorder([]string{"nonexistent-id"})
	if err != ErrInvalidItemIDs {
		t.Fatalf("Reorder() error = %v, want ErrInvalidItemIDs", err)
	}
}

func TestGetStateReturnsCopy(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()
	rq.Add(QueueItem{WorkOrderFile: "wo1.yaml", Title: "First"})

	snap := rq.GetState()
	snap.Items[0].Title = "Modified"

	snap2 := rq.GetState()
	if snap2.Items[0].Title != "First" {
		t.Fatalf("GetState() returned shared reference, title = %q, want %q", snap2.Items[0].Title, "First")
	}
}

func TestStartRequiresReady(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()
	err := rq.Start()
	if err != ErrQueueInvalidState {
		t.Fatalf("Start() error = %v, want ErrQueueInvalidState", err)
	}
}

func TestStartRequiresConfiguredCallbacks(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()
	rq.Add(QueueItem{WorkOrderFile: "wo1.yaml", Title: "First"})

	err := rq.Start()
	if err != ErrQueueNotConfigured {
		t.Fatalf("Start() error = %v, want ErrQueueNotConfigured", err)
	}
}

func TestStartTransitionsToRunning(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()
	rq.SetCallbacks(
		func(item QueueItem) (string, error) {
			return "wf-1", nil
		},
		func(workflowID string) (string, error) {
			return "completed", nil
		},
	)
	rq.Add(QueueItem{WorkOrderFile: "wo1.yaml", Title: "First"})

	if err := rq.Start(); err != nil {
		t.Fatalf("Start() error: %v", err)
	}

	snap := waitForState(t, rq, QueueStateCompleted)
	if snap.State != QueueStateCompleted {
		t.Fatalf("state = %q, want %q", snap.State, QueueStateCompleted)
	}
}

func TestPauseRequiresRunning(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()
	err := rq.Pause()
	if err != ErrQueueInvalidState {
		t.Fatalf("Pause() error = %v, want ErrQueueInvalidState", err)
	}
}

func TestContinueRequiresPaused(t *testing.T) {
	t.Parallel()

	rq := NewRunQueue()
	err := rq.Continue()
	if err != ErrQueueInvalidState {
		t.Fatalf("Continue() error = %v, want ErrQueueInvalidState", err)
	}
}

// waitForState polls the queue until the desired state is reached or times out.
func waitForState(t *testing.T, rq *RunQueue, desired string) QueueSnapshot {
	t.Helper()
	for range 50 {
		snap := rq.GetState()
		if snap.State == desired {
			return snap
		}
		time.Sleep(20 * time.Millisecond)
	}
	snap := rq.GetState()
	t.Fatalf("timed out waiting for state %q, current = %q", desired, snap.State)
	return snap
}
