package api

import (
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Queue states.
const (
	QueueStateIdle      = "idle"
	QueueStateReady     = "ready"
	QueueStateRunning   = "running"
	QueueStatePaused    = "paused"
	QueueStateCompleted = "completed"
)

// Queue item statuses.
const (
	QueueItemPending   = "pending"
	QueueItemExecuting = "executing"
	QueueItemCompleted = "completed"
	QueueItemFailed    = "failed"
	QueueItemSkipped   = "skipped"
)

// QueueItem represents a single work order in the run queue.
type QueueItem struct {
	ID            string
	WorkOrderFile string
	Title         string
	Type          string
	TargetModule  string
	Overrides     map[string]string
	Status        string
	WorkflowID    string
	Error         string
	AddedAt       time.Time
}

// QueueSnapshot is a point-in-time copy of the queue state.
type QueueSnapshot struct {
	State       string
	Items       []QueueItem
	Current     string
	PauseReason string
}

// ExecuteFn is the callback invoked to execute a queue item.
// It receives the item and should return a workflow ID.
type ExecuteFn func(item QueueItem) (workflowID string, err error)

// MonitorFn is the callback invoked to monitor a running workflow.
// It should block until the workflow completes and return its terminal state.
type MonitorFn func(workflowID string) (terminalState string, err error)

// QueueEvent is emitted when the queue state changes.
type QueueEvent struct {
	Type string
	Data any
}

var (
	ErrQueueInvalidState = errors.New("invalid queue state for operation")
	ErrItemNotFound      = errors.New("queue item not found")
	ErrItemExecuting     = errors.New("cannot remove executing item")
	ErrInvalidItemIDs    = errors.New("invalid item IDs")
)

// RunQueue is an in-memory queue that manages sequential execution of work orders.
type RunQueue struct {
	mu          sync.Mutex
	state       string
	items       []QueueItem
	current     string
	pauseReason string
	executeFn   ExecuteFn
	monitorFn   MonitorFn

	subscribers map[chan QueueEvent]struct{}
}

// NewRunQueue creates a new RunQueue in idle state.
func NewRunQueue() *RunQueue {
	return &RunQueue{
		state:       QueueStateIdle,
		subscribers: make(map[chan QueueEvent]struct{}),
	}
}

// SetCallbacks configures the execute and monitor callbacks.
func (rq *RunQueue) SetCallbacks(executeFn ExecuteFn, monitorFn MonitorFn) {
	rq.mu.Lock()
	defer rq.mu.Unlock()
	rq.executeFn = executeFn
	rq.monitorFn = monitorFn
}

// Add appends a new item to the queue and transitions idle -> ready.
func (rq *RunQueue) Add(item QueueItem) QueueItem {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	item.ID = uuid.New().String()
	item.Status = QueueItemPending
	item.AddedAt = time.Now().UTC()
	rq.items = append(rq.items, item)

	if rq.state == QueueStateIdle || rq.state == QueueStateCompleted {
		rq.state = QueueStateReady
	}

	rq.broadcast(QueueEvent{Type: "queue_state_changed"})
	return item
}

// Remove removes a pending item by ID.
// Returns ErrItemExecuting if the item is currently executing.
// Returns ErrItemNotFound if the item does not exist.
func (rq *RunQueue) Remove(id string) error {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	for i, item := range rq.items {
		if item.ID == id {
			if item.Status == QueueItemExecuting {
				return ErrItemExecuting
			}
			rq.items = append(rq.items[:i], rq.items[i+1:]...)

			// If no pending items remain and not running, go idle.
			if !rq.hasPending() && rq.state == QueueStateReady {
				rq.state = QueueStateIdle
			}

			rq.broadcast(QueueEvent{Type: "queue_state_changed"})
			return nil
		}
	}

	return ErrItemNotFound
}

// Reorder reorders pending items to match the given ID order.
// All provided IDs must correspond to existing pending items.
func (rq *RunQueue) Reorder(ids []string) error {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	// Separate executing/completed items from pending ones.
	pendingByID := make(map[string]QueueItem, len(rq.items))
	var nonPending []QueueItem
	for _, item := range rq.items {
		if item.Status == QueueItemPending {
			pendingByID[item.ID] = item
		} else {
			nonPending = append(nonPending, item)
		}
	}

	// Validate that all IDs are valid pending items.
	if len(ids) != len(pendingByID) {
		return ErrInvalidItemIDs
	}
	for _, id := range ids {
		if _, ok := pendingByID[id]; !ok {
			return ErrInvalidItemIDs
		}
	}

	// Rebuild: non-pending items first, then reordered pending items.
	reordered := make([]QueueItem, 0, len(rq.items))
	reordered = append(reordered, nonPending...)
	for _, id := range ids {
		reordered = append(reordered, pendingByID[id])
	}
	rq.items = reordered

	rq.broadcast(QueueEvent{Type: "queue_state_changed"})
	return nil
}

// GetState returns a snapshot copy of the current queue state.
func (rq *RunQueue) GetState() QueueSnapshot {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	items := make([]QueueItem, len(rq.items))
	copy(items, rq.items)

	return QueueSnapshot{
		State:       rq.state,
		Items:       items,
		Current:     rq.current,
		PauseReason: rq.pauseReason,
	}
}

// Start begins executing the queue. Requires state == ready.
func (rq *RunQueue) Start() error {
	rq.mu.Lock()
	if rq.state != QueueStateReady {
		rq.mu.Unlock()
		return ErrQueueInvalidState
	}
	rq.state = QueueStateRunning
	rq.pauseReason = ""
	rq.mu.Unlock()

	rq.broadcast(QueueEvent{Type: "queue_state_changed"})
	go rq.runLoop()
	return nil
}

// Pause requests the queue to pause after the current item completes.
// Requires state == running.
func (rq *RunQueue) Pause() error {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if rq.state != QueueStateRunning {
		return ErrQueueInvalidState
	}

	rq.state = QueueStatePaused
	rq.pauseReason = "user_requested"

	rq.broadcast(QueueEvent{Type: "queue_state_changed"})
	return nil
}

// Continue resumes the queue from a paused state.
// If the current item is executing (awaiting review), marks it completed and advances.
func (rq *RunQueue) Continue() error {
	rq.mu.Lock()
	if rq.state != QueueStatePaused {
		rq.mu.Unlock()
		return ErrQueueInvalidState
	}

	// If there's a current executing item in paused state (awaiting review), mark completed.
	for i, item := range rq.items {
		if item.ID == rq.current && item.Status == QueueItemExecuting {
			rq.items[i].Status = QueueItemCompleted
			break
		}
	}

	rq.state = QueueStateRunning
	rq.current = ""
	rq.pauseReason = ""
	rq.mu.Unlock()

	rq.broadcast(QueueEvent{Type: "queue_state_changed"})
	go rq.runLoop()
	return nil
}

// Subscribe returns a channel that receives queue events.
func (rq *RunQueue) Subscribe() chan QueueEvent {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	ch := make(chan QueueEvent, 16)
	rq.subscribers[ch] = struct{}{}
	return ch
}

// Unsubscribe removes and closes a subscriber channel.
func (rq *RunQueue) Unsubscribe(ch chan QueueEvent) {
	rq.mu.Lock()
	defer rq.mu.Unlock()

	if _, ok := rq.subscribers[ch]; ok {
		delete(rq.subscribers, ch)
		close(ch)
	}
}

// broadcast sends an event to all subscribers. Drops for slow consumers.
// Must NOT hold rq.mu when called from outside locked context.
// When called from within a locked context, the caller must be aware
// that slow consumers will have their events dropped.
func (rq *RunQueue) broadcast(event QueueEvent) {
	for ch := range rq.subscribers {
		select {
		case ch <- event:
		default:
			// Drop for slow consumer.
		}
	}
}

// hasPending reports whether any item has pending status.
// Must be called with rq.mu held.
func (rq *RunQueue) hasPending() bool {
	for _, item := range rq.items {
		if item.Status == QueueItemPending {
			return true
		}
	}
	return false
}

// runLoop processes queue items sequentially.
func (rq *RunQueue) runLoop() {
	for {
		rq.mu.Lock()
		if rq.state != QueueStateRunning {
			rq.mu.Unlock()
			return
		}

		// Find next pending item.
		idx := -1
		for i, item := range rq.items {
			if item.Status == QueueItemPending {
				idx = i
				break
			}
		}

		if idx == -1 {
			// No more pending items - queue is completed.
			rq.state = QueueStateCompleted
			rq.current = ""
			rq.mu.Unlock()
			rq.broadcast(QueueEvent{Type: "queue_state_changed"})
			return
		}

		rq.items[idx].Status = QueueItemExecuting
		rq.current = rq.items[idx].ID
		itemCopy := rq.items[idx]
		rq.mu.Unlock()

		rq.broadcast(QueueEvent{Type: "queue_item_started", Data: itemCopy.ID})

		// Execute the item.
		var workflowID string
		var execErr error

		if rq.executeFn != nil {
			workflowID, execErr = rq.executeFn(itemCopy)
		}

		if execErr != nil {
			rq.mu.Lock()
			for i, item := range rq.items {
				if item.ID == itemCopy.ID {
					rq.items[i].Status = QueueItemFailed
					rq.items[i].Error = execErr.Error()
					break
				}
			}
			rq.state = QueueStatePaused
			rq.pauseReason = "error"
			rq.current = ""
			rq.mu.Unlock()
			rq.broadcast(QueueEvent{Type: "queue_item_completed", Data: itemCopy.ID})
			rq.broadcast(QueueEvent{Type: "queue_state_changed"})
			return
		}

		// Update workflow ID.
		rq.mu.Lock()
		for i, item := range rq.items {
			if item.ID == itemCopy.ID {
				rq.items[i].WorkflowID = workflowID
				break
			}
		}
		rq.mu.Unlock()

		// Monitor the workflow if we have a monitor function.
		if rq.monitorFn != nil && workflowID != "" {
			terminalState, monErr := rq.monitorFn(workflowID)
			if monErr != nil {
				rq.mu.Lock()
				for i, item := range rq.items {
					if item.ID == itemCopy.ID {
						rq.items[i].Status = QueueItemFailed
						rq.items[i].Error = monErr.Error()
						break
					}
				}
				rq.state = QueueStatePaused
				rq.pauseReason = "error"
				rq.current = ""
				rq.mu.Unlock()
				rq.broadcast(QueueEvent{Type: "queue_item_completed", Data: itemCopy.ID})
				rq.broadcast(QueueEvent{Type: "queue_state_changed"})
				return
			}

			if terminalState == "human_review" {
				// Auto-pause for human review.
				rq.mu.Lock()
				rq.state = QueueStatePaused
				rq.pauseReason = "awaiting_review"
				rq.mu.Unlock()
				rq.broadcast(QueueEvent{Type: "queue_state_changed"})
				return
			}
		}

		// Mark completed and advance.
		rq.mu.Lock()
		for i, item := range rq.items {
			if item.ID == itemCopy.ID {
				rq.items[i].Status = QueueItemCompleted
				break
			}
		}
		rq.current = ""
		rq.mu.Unlock()

		rq.broadcast(QueueEvent{Type: "queue_item_completed", Data: itemCopy.ID})

		// Check if paused before continuing to next item.
		rq.mu.Lock()
		if rq.state != QueueStateRunning {
			rq.mu.Unlock()
			return
		}
		rq.mu.Unlock()
	}
}
