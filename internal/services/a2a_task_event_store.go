package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"invoice-backend/pkg/a2a"

	"github.com/google/uuid"
)

type A2ATaskEvent struct {
	Sequence  int64           `gorm:"primaryKey;autoIncrement"`
	TaskID    string          `gorm:"type:uuid;not null;index:idx_a2a_task_events_task_sequence,priority:1"`
	EventID   string          `gorm:"type:varchar(64);not null;uniqueIndex"`
	EventType string          `gorm:"type:varchar(32);not null"`
	Data      json.RawMessage `gorm:"type:jsonb;not null"`
	CreatedAt time.Time       `gorm:"autoCreateTime;index:idx_a2a_task_events_task_sequence,priority:2"`
}

func (A2ATaskEvent) TableName() string {
	return "a2a_task_events"
}

func (s *A2ATaskService) recordStreamEvent(ctx context.Context, taskID string, event a2a.StreamEvent) (a2a.StreamEvent, error) {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	record := &A2ATaskEvent{
		TaskID:    taskID,
		EventID:   event.ID,
		EventType: event.Event,
		Data:      event.Data,
		CreatedAt: event.Timestamp.UTC(),
	}

	if err := s.db.WithContext(ctx).Create(record).Error; err != nil {
		return event, fmt.Errorf("persist task stream event: %w", err)
	}

	return event, nil
}

func (s *A2ATaskService) loadTaskEventsAfter(ctx context.Context, taskID, lastEventID string, limit int) ([]a2a.StreamEvent, error) {
	query := s.db.WithContext(ctx).Model(&A2ATaskEvent{}).Where("task_id = ?", taskID)

	if lastEventID != "" {
		var last A2ATaskEvent
		err := s.db.WithContext(ctx).Where("task_id = ? AND event_id = ?", taskID, lastEventID).First(&last).Error
		if err == nil {
			query = query.Where("sequence > ?", last.Sequence)
		}
	}

	if limit <= 0 {
		limit = 200
	}

	var records []A2ATaskEvent
	if err := query.Order("sequence ASC").Limit(limit).Find(&records).Error; err != nil {
		return nil, fmt.Errorf("load task stream events: %w", err)
	}

	events := make([]a2a.StreamEvent, 0, len(records))
	for _, rec := range records {
		final := false
		if rec.EventType == a2a.StreamEventStatusUpdate {
			var payload a2a.StreamResponse
			if err := json.Unmarshal(rec.Data, &payload); err == nil && payload.StatusUpdate != nil {
				final = payload.StatusUpdate.Final
			}
		}
		events = append(events, a2a.StreamEvent{
			ID:        rec.EventID,
			Event:     rec.EventType,
			Data:      rec.Data,
			Timestamp: rec.CreatedAt,
			Final:     final,
		})
	}

	return events, nil
}
