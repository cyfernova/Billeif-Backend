package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"invoice-backend/pkg/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newWorkflowTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	statements := []string{
		`CREATE TABLE workflows (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			user_id TEXT NOT NULL,
			agent_id TEXT,
			trigger BLOB NOT NULL,
			action BLOB NOT NULL,
			status TEXT,
			is_enabled BOOLEAN,
			last_run DATETIME,
			next_run DATETIME,
			run_count INTEGER,
			success_count INTEGER,
			failure_count INTEGER,
			last_error TEXT,
			notification_settings BLOB,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE workflow_runs (
			id TEXT PRIMARY KEY,
			workflow_id TEXT NOT NULL,
			trigger_type TEXT,
			trigger_data BLOB,
			status TEXT,
			result BLOB,
			error_message TEXT,
			started_at DATETIME,
			completed_at DATETIME,
			duration_ms INTEGER
		)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create workflow schema: %v", err)
		}
	}
	return db
}

func TestWorkflowServiceDuplicateWorkflowResetsRunState(t *testing.T) {
	db := newWorkflowTestDB(t)
	svc := NewWorkflowService(db, logger.New(), nil, nil)

	schedule := "0 9 * * *"
	original := &Workflow{
		ID:                   "workflow-1",
		Name:                 "Morning pricing",
		Description:          "Original workflow",
		UserID:               "user-1",
		Trigger:              &WorkflowTrigger{Type: TriggerTypeTime, Schedule: &schedule},
		Action:               &WorkflowAction{Type: WorkflowActionNotify, Message: "hello", Recipients: []string{"ops@example.com"}},
		Status:               WorkflowStatusActive,
		IsEnabled:            true,
		LastRun:              ptrTime(time.Now().UTC()),
		NextRun:              ptrTime(time.Now().UTC().Add(time.Hour)),
		RunCount:             8,
		SuccessCount:         7,
		FailureCount:         1,
		LastError:            ptrString("last error"),
		NotificationSettings: json.RawMessage(`{"email":true}`),
	}
	if err := db.Create(original).Error; err != nil {
		t.Fatalf("create original workflow: %v", err)
	}

	duplicate, err := svc.DuplicateWorkflow(context.Background(), original.ID, "user-1")
	if err != nil {
		t.Fatalf("duplicate workflow: %v", err)
	}

	if duplicate.ID == original.ID {
		t.Fatal("expected duplicate to get a new ID")
	}
	if duplicate.Name != "Copy of Morning pricing" {
		t.Fatalf("unexpected duplicate name: %q", duplicate.Name)
	}
	if duplicate.Status != WorkflowStatusPaused || duplicate.IsEnabled {
		t.Fatalf("expected paused disabled copy, got status=%s enabled=%v", duplicate.Status, duplicate.IsEnabled)
	}
	if duplicate.RunCount != 0 || duplicate.SuccessCount != 0 || duplicate.FailureCount != 0 || duplicate.LastRun != nil || duplicate.NextRun != nil || duplicate.LastError != nil {
		t.Fatalf("expected run state to be reset: %+v", duplicate)
	}
	if duplicate.Trigger == nil || duplicate.Trigger.Schedule == nil || *duplicate.Trigger.Schedule != schedule {
		t.Fatal("expected trigger to be copied")
	}
	if duplicate.Action == nil || duplicate.Action.Message != "hello" {
		t.Fatal("expected action to be copied")
	}
}

func TestWorkflowServiceDuplicateWorkflowRejectsWrongOwner(t *testing.T) {
	db := newWorkflowTestDB(t)
	svc := NewWorkflowService(db, logger.New(), nil, nil)

	workflow := &Workflow{
		ID:        "workflow-2",
		Name:      "Owned workflow",
		UserID:    "user-1",
		Trigger:   &WorkflowTrigger{Type: TriggerTypePrice},
		Action:    &WorkflowAction{Type: WorkflowActionNotify},
		Status:    WorkflowStatusPaused,
		IsEnabled: false,
	}
	if err := db.Create(workflow).Error; err != nil {
		t.Fatalf("create workflow: %v", err)
	}

	duplicate, err := svc.DuplicateWorkflow(context.Background(), workflow.ID, "user-2")
	if err == nil {
		t.Fatal("expected ownership error")
	}
	if duplicate != nil {
		t.Fatalf("expected no duplicate, got %+v", duplicate)
	}
}

func ptrString(value string) *string {
	return &value
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
