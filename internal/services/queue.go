package services

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// QueueItem is one message sitting in the Postfix queue.
type QueueItem struct {
	QueueID    string   `json:"queue_id"`
	Sender     string   `json:"sender"`
	Recipients []string `json:"recipients"`
	SizeBytes  int64    `json:"size_bytes"`
	ArrivedAt  string   `json:"arrived_at"`
	Reason     string   `json:"reason"` // last delivery error, if deferred
	Queue      string   `json:"queue"`  // active | deferred | hold | incoming
}

// QueueInspector reads and pokes the outbound mail queue.
type QueueInspector interface {
	List(ctx context.Context) ([]QueueItem, error)
	Count(ctx context.Context) (int, error)
	Flush(ctx context.Context) error                         // ask Postfix to retry everything now
	Retry(ctx context.Context, queueID string) error         // retry one message
	DeleteMessage(ctx context.Context, queueID string) error // remove one message
}

// LimitedQueueInspector supports cheaper summary views.
type LimitedQueueInspector interface {
	ListLimit(ctx context.Context, keep int) ([]QueueItem, error)
}

// ---- postfix (production) ----

// PostfixQueue shells out to postqueue/postsuper.
type PostfixQueue struct{}

func NewPostfixQueue() *PostfixQueue { return &PostfixQueue{} }

const maxQueueListItems = 200

func (q *PostfixQueue) List(ctx context.Context) ([]QueueItem, error) {
	return q.ListLimit(ctx, maxQueueListItems)
}

func (q *PostfixQueue) ListLimit(ctx context.Context, keep int) ([]QueueItem, error) {
	if keep <= 0 || keep > maxQueueListItems {
		keep = maxQueueListItems
	}
	scan, err := q.scan(ctx, keep)
	return scan.Items, err
}

func (q *PostfixQueue) Count(ctx context.Context) (int, error) {
	scan, err := q.scan(ctx, 0)
	return scan.Count, err
}

func (q *PostfixQueue) scan(ctx context.Context, keep int) (queueScan, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "postqueue", "-j")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return queueScan{}, fmt.Errorf("postqueue -j: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return queueScan{}, fmt.Errorf("postqueue -j: %w", err)
	}
	scan, scanErr := scanPostqueueJSON(stdout, keep)
	waitErr := cmd.Wait()
	if waitErr != nil {
		// An empty queue exits 0 with no output; a real error surfaces here.
		return queueScan{}, fmt.Errorf("postqueue -j: %w", waitErr)
	}
	if scanErr != nil {
		return queueScan{}, scanErr
	}
	return scan, nil
}

type queueScan struct {
	Items []QueueItem
	Count int
}

// parsePostqueueJSON parses `postqueue -j` output (one JSON object per line).
func parsePostqueueJSON(out []byte) ([]QueueItem, error) {
	scan, err := scanPostqueueJSON(bytes.NewReader(out), -1)
	if err != nil {
		return nil, err
	}
	return scan.Items, nil
}

func scanPostqueueJSON(r io.Reader, keep int) (queueScan, error) {
	var out queueScan
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 256*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var raw struct {
			QueueName   string `json:"queue_name"`
			QueueID     string `json:"queue_id"`
			ArrivalTime int64  `json:"arrival_time"`
			MessageSize int64  `json:"message_size"`
			Sender      string `json:"sender"`
			Recipients  []struct {
				Address     string `json:"address"`
				DelayReason string `json:"delay_reason"`
			} `json:"recipients"`
		}
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue // tolerate partial lines rather than failing the whole view
		}
		out.Count++
		item := QueueItem{
			QueueID:   raw.QueueID,
			Sender:    raw.Sender,
			SizeBytes: raw.MessageSize,
			ArrivedAt: time.Unix(raw.ArrivalTime, 0).UTC().Format(time.RFC3339),
			Queue:     raw.QueueName,
		}
		for _, r := range raw.Recipients {
			item.Recipients = append(item.Recipients, r.Address)
			if r.DelayReason != "" {
				item.Reason = r.DelayReason
			}
		}
		if keep < 0 || len(out.Items) < keep {
			out.Items = append(out.Items, item)
		}
	}
	return out, sc.Err()
}

func (q *PostfixQueue) Flush(ctx context.Context) error {
	return exec.CommandContext(ctx, "postqueue", "-f").Run()
}

func (q *PostfixQueue) Retry(ctx context.Context, queueID string) error {
	if !validQueueID(queueID) {
		return fmt.Errorf("invalid queue id")
	}
	return exec.CommandContext(ctx, "postqueue", "-i", queueID).Run()
}

func (q *PostfixQueue) DeleteMessage(ctx context.Context, queueID string) error {
	if !validQueueID(queueID) {
		return fmt.Errorf("invalid queue id")
	}
	// postsuper requires root; covered by the installer's sudoers allowlist.
	if os.Geteuid() == 0 {
		return exec.CommandContext(ctx, "postsuper", "-d", queueID).Run()
	}
	return exec.CommandContext(ctx, "sudo", "-n", "postsuper", "-d", queueID).Run()
}

func validQueueID(id string) bool {
	if id == "" || len(id) > 20 {
		return false
	}
	for _, c := range id {
		if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	return true
}

// ---- mock ----

// MockQueue simulates a small queue for dev mode and tests.
type MockQueue struct {
	mu    sync.Mutex
	Items []QueueItem
}

func NewMockQueue() *MockQueue {
	return &MockQueue{Items: []QueueItem{
		{
			QueueID: "DEV1MOCK01", Sender: "hello@example.com",
			Recipients: []string{"friend@elsewhere.net"}, SizeBytes: 4213,
			ArrivedAt: time.Now().Add(-25 * time.Minute).UTC().Format(time.RFC3339),
			Reason:    "connect to elsewhere.net[203.0.113.9]:25: Connection timed out",
			Queue:     "deferred",
		},
	}}
}

func (q *MockQueue) List(_ context.Context) ([]QueueItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]QueueItem(nil), q.Items...), nil
}

func (q *MockQueue) ListLimit(_ context.Context, keep int) ([]QueueItem, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if keep <= 0 || keep > len(q.Items) {
		keep = len(q.Items)
	}
	return append([]QueueItem(nil), q.Items[:keep]...), nil
}

func (q *MockQueue) Count(ctx context.Context) (int, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.Items), nil
}

func (q *MockQueue) Flush(_ context.Context) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.Items {
		q.Items[i].Queue = "active"
		q.Items[i].Reason = ""
	}
	return nil
}

func (q *MockQueue) Retry(_ context.Context, queueID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.Items {
		if q.Items[i].QueueID == queueID {
			q.Items[i].Queue = "active"
			q.Items[i].Reason = ""
			return nil
		}
	}
	return fmt.Errorf("message %s not found in queue", queueID)
}

func (q *MockQueue) DeleteMessage(_ context.Context, queueID string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	for i := range q.Items {
		if q.Items[i].QueueID == queueID {
			q.Items = append(q.Items[:i], q.Items[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("message %s not found in queue", queueID)
}
