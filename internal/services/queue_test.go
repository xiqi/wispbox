package services

import (
	"context"
	"testing"
)

func TestMockQueueFlushMarksQueuedMailActive(t *testing.T) {
	q := NewMockQueue()

	if err := q.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	items, err := q.List(context.Background())
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].Queue != "active" {
		t.Fatalf("Queue = %q, want active", items[0].Queue)
	}
	if items[0].Reason != "" {
		t.Fatalf("Reason = %q, want empty", items[0].Reason)
	}
}
