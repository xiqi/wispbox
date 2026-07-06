package services

import (
	"context"
	"fmt"
	"strings"
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

func TestScanPostqueueJSONCountsWithoutKeepingEverything(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 3; i++ {
		fmt.Fprintf(&b, `{"queue_name":"deferred","queue_id":"ID%d","arrival_time":1,"message_size":42,"sender":"a@example.com","recipients":[{"address":"b@example.com","delay_reason":"later"}]}`+"\n", i)
	}

	scan, err := scanPostqueueJSON(strings.NewReader(b.String()), 1)
	if err != nil {
		t.Fatalf("scanPostqueueJSON() error = %v", err)
	}
	if scan.Count != 3 {
		t.Fatalf("Count = %d, want 3", scan.Count)
	}
	if len(scan.Items) != 1 {
		t.Fatalf("len(Items) = %d, want 1", len(scan.Items))
	}
}
