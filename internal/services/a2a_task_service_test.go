package services

import (
	"context"
	"fmt"
	"testing"
	"time"

	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newTestA2ATaskService(t *testing.T) (*A2ATaskService, *A2APushService, *gorm.DB) {
	t.Helper()

	dsn := fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	if err := createSQLiteA2ATestSchema(db); err != nil {
		t.Fatalf("create sqlite a2a schema: %v", err)
	}

	log := logger.New()
	pushService := NewA2APushService(db, nil, log)
	taskService := NewA2ATaskService(db, log, pushService)
	return taskService, pushService, db
}

func createSQLiteA2ATestSchema(db *gorm.DB) error {
	statements := []string{
		`CREATE TABLE a2a_tasks (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			state TEXT NOT NULL,
			artifacts TEXT NOT NULL DEFAULT '[]',
			messages TEXT NOT NULL DEFAULT '[]',
			metadata TEXT NOT NULL DEFAULT '{}',
			user_id TEXT,
			business_id TEXT,
			created_at DATETIME,
			updated_at DATETIME
		);`,
		`CREATE TABLE a2a_task_events (
			sequence INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id TEXT NOT NULL,
			event_id TEXT NOT NULL UNIQUE,
			event_type TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at DATETIME NOT NULL
		);`,
		`CREATE TABLE a2a_task_push_configs (
			id TEXT PRIMARY KEY,
			task_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			business_id TEXT,
			url TEXT NOT NULL,
			token TEXT,
			authentication TEXT NOT NULL DEFAULT '{}',
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		);`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			return err
		}
	}
	return nil
}

func TestA2ATaskServiceListTasksPaginationAndScoping(t *testing.T) {
	taskService, _, _ := newTestA2ATaskService(t)

	ctx := context.Background()
	makeTask := func(userID, businessID string, createdAt time.Time) {
		task := a2a.NewTask("", userID, businessID)
		task.CreatedAt = createdAt
		task.UpdatedAt = createdAt
		if err := taskService.saveTask(ctx, task); err != nil {
			t.Fatalf("save task: %v", err)
		}
	}

	now := time.Now().UTC()
	makeTask("user-1", "biz-1", now.Add(-3*time.Minute))
	makeTask("user-1", "biz-1", now.Add(-2*time.Minute))
	makeTask("user-2", "biz-1", now.Add(-1*time.Minute))

	pageSize := 1
	resp, err := taskService.ListTasks(ctx, &a2a.ListTasksRequest{PageSize: &pageSize}, "user-1", "biz-1")
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(resp.Tasks) != 1 {
		t.Fatalf("expected 1 task on first page, got %d", len(resp.Tasks))
	}
	if resp.TotalSize != 2 {
		t.Fatalf("expected total size 2, got %d", resp.TotalSize)
	}
	if resp.NextPageToken == "" {
		t.Fatal("expected next page token on first page")
	}

	secondPage, err := taskService.ListTasks(ctx, &a2a.ListTasksRequest{
		PageSize:  &pageSize,
		PageToken: resp.NextPageToken,
	}, "user-1", "biz-1")
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(secondPage.Tasks) != 1 {
		t.Fatalf("expected 1 task on second page, got %d", len(secondPage.Tasks))
	}
	if secondPage.Tasks[0].ID == resp.Tasks[0].ID {
		t.Fatal("expected second page to contain a different task")
	}
}

func TestA2ATaskServiceCancelTask(t *testing.T) {
	taskService, _, _ := newTestA2ATaskService(t)

	task := a2a.NewTask("", "user-1", "biz-1")
	if err := taskService.saveTask(context.Background(), task); err != nil {
		t.Fatalf("save task: %v", err)
	}

	cancelled, err := taskService.CancelTask(context.Background(), task.ID, "user-1", "biz-1")
	if err != nil {
		t.Fatalf("cancel task: %v", err)
	}
	if cancelled.Status.State != a2a.TaskStateCancelled {
		t.Fatalf("expected cancelled task state, got %s", cancelled.Status.State)
	}
}

func TestA2ATaskServiceSubscribeTaskWithReplayUsesDurableEvents(t *testing.T) {
	taskService, _, _ := newTestA2ATaskService(t)

	task := a2a.NewTask("", "user-1", "biz-1")
	working := a2a.NewTextMessage(a2a.RoleAgent, "Task is working.")
	if err := task.SetState(a2a.TaskStateWorking, &working); err != nil {
		t.Fatalf("set working state: %v", err)
	}
	if err := taskService.saveTask(context.Background(), task); err != nil {
		t.Fatalf("save task: %v", err)
	}

	eventPayload, err := a2a.MarshalStreamResponse(a2a.StreamResponse{
		StatusUpdate: &a2a.TaskStatusUpdateEvent{
			TaskID:    task.ID,
			ContextID: task.ContextID,
			Status:    task.Status,
			Final:     false,
		},
	})
	if err != nil {
		t.Fatalf("marshal stream response: %v", err)
	}
	recorded, err := taskService.recordStreamEvent(context.Background(), task.ID, a2a.StreamEvent{
		ID:        "evt-1",
		Event:     a2a.StreamEventStatusUpdate,
		Data:      eventPayload,
		Timestamp: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("record stream event: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	events := make(chan a2a.StreamEvent, 4)
	go func() {
		_ = taskService.SubscribeTaskWithReplay(ctx, task.ID, "user-1", "biz-1", "", nil, true, events)
	}()

	select {
	case event := <-events:
		if event.ID != recorded.ID {
			t.Fatalf("expected replayed event %q, got %q", recorded.ID, event.ID)
		}
		if event.Event != a2a.StreamEventStatusUpdate {
			t.Fatalf("expected status-update replay, got %s", event.Event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for replayed event")
	}

	cancel()
}
