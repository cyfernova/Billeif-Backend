package services

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

type memoryNotificationRepository struct {
	mu      sync.Mutex
	records map[string]*models.Notification
	nextID  int
}

func newMemoryNotificationRepository() *memoryNotificationRepository {
	return &memoryNotificationRepository{records: make(map[string]*models.Notification)}
}

func (r *memoryNotificationRepository) CreateIdempotent(_ context.Context, notification *models.Notification) (*models.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := notification.BusinessID + ":" + notification.UserID + ":" + notification.SourceEventKey
	if existing := r.records[key]; existing != nil {
		copy := *existing
		return &copy, nil
	}
	r.nextID++
	copy := *notification
	copy.ID = "notification-" + string(rune('0'+r.nextID))
	r.records[key] = &copy
	return &copy, nil
}

func (r *memoryNotificationRepository) List(_ context.Context, businessID, userID string, limit int) ([]*models.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]*models.Notification, 0)
	for _, notification := range r.records {
		if notification.BusinessID == businessID && notification.UserID == userID {
			copy := *notification
			result = append(result, &copy)
		}
	}
	if len(result) > limit {
		result = result[:limit]
	}
	return result, nil
}

func (r *memoryNotificationRepository) MarkRead(_ context.Context, id, businessID, userID string, readAt time.Time) (*models.Notification, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, notification := range r.records {
		if notification.ID == id && notification.BusinessID == businessID && notification.UserID == userID {
			copy := *notification
			copy.ReadAt = &readAt
			r.records[key] = &copy
			return &copy, nil
		}
	}
	return nil, interfaces.ErrNotificationNotFound
}

func (r *memoryNotificationRepository) MarkAllRead(_ context.Context, businessID, userID string, readAt time.Time) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var updated int64
	for key, notification := range r.records {
		if notification.BusinessID == businessID && notification.UserID == userID && notification.ReadAt == nil {
			copy := *notification
			copy.ReadAt = &readAt
			r.records[key] = &copy
			updated++
		}
	}
	return updated, nil
}

func TestNotificationIngestionIsIdempotentByTenantUserAndEvent(t *testing.T) {
	now := time.Date(2026, time.August, 31, 13, 0, 0, 0, time.UTC)
	repository := newMemoryNotificationRepository()
	service := NewNotificationService(repository, NotificationServiceOptions{Now: func() time.Time { return now }})
	input := NotificationInput{
		BusinessID:     "business-a",
		UserID:         "user-a",
		SourceEventKey: "invoice:issued:invoice-1",
		Type:           "invoice",
		Title:          "Invoice issued",
		Body:           "Invoice INV-1 is ready.",
		ResourceType:   "invoice",
		ResourceID:     "invoice-1",
	}

	first, err := service.Ingest(context.Background(), input)
	if err != nil {
		t.Fatalf("first Ingest() error = %v", err)
	}
	second, err := service.Ingest(context.Background(), input)
	if err != nil {
		t.Fatalf("second Ingest() error = %v", err)
	}
	if first.ID == "" || second.ID != first.ID || len(repository.records) != 1 {
		t.Fatalf("idempotent records = first %#v second %#v count %d", first, second, len(repository.records))
	}
}

func TestNotificationListAndReadAreScopedToBusinessAndUser(t *testing.T) {
	now := time.Date(2026, time.August, 31, 13, 0, 0, 0, time.UTC)
	repository := newMemoryNotificationRepository()
	service := NewNotificationService(repository, NotificationServiceOptions{Now: func() time.Time { return now }})
	for _, input := range []NotificationInput{
		{BusinessID: "business-a", UserID: "user-a", SourceEventKey: "event-a", Type: "system", Title: "A", Body: "A"},
		{BusinessID: "business-b", UserID: "user-a", SourceEventKey: "event-b", Type: "system", Title: "B", Body: "B"},
		{BusinessID: "business-a", UserID: "user-b", SourceEventKey: "event-c", Type: "system", Title: "C", Body: "C"},
	} {
		if _, err := service.Ingest(context.Background(), input); err != nil {
			t.Fatalf("Ingest(%s) error = %v", input.SourceEventKey, err)
		}
	}

	listed, err := service.List(context.Background(), "business-a", "user-a", 100)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(listed) != 1 || listed[0].BusinessID != "business-a" || listed[0].UserID != "user-a" {
		t.Fatalf("scoped list = %#v", listed)
	}
	if _, err := service.MarkRead(context.Background(), listed[0].ID, "business-b", "user-a"); !errors.Is(err, interfaces.ErrNotificationNotFound) {
		t.Fatalf("cross-business MarkRead() error = %v", err)
	}
	if _, err := service.MarkRead(context.Background(), listed[0].ID, "business-a", "user-b"); !errors.Is(err, interfaces.ErrNotificationNotFound) {
		t.Fatalf("cross-user MarkRead() error = %v", err)
	}
	read, err := service.MarkRead(context.Background(), listed[0].ID, "business-a", "user-a")
	if err != nil || read.ReadAt == nil || !read.ReadAt.Equal(now) {
		t.Fatalf("scoped MarkRead() = %#v, %v", read, err)
	}
}

func TestNotificationReadAllOnlyMutatesCurrentBusinessAndUser(t *testing.T) {
	now := time.Date(2026, time.August, 31, 13, 0, 0, 0, time.UTC)
	repository := newMemoryNotificationRepository()
	service := NewNotificationService(repository, NotificationServiceOptions{Now: func() time.Time { return now }})
	for _, input := range []NotificationInput{
		{BusinessID: "business-a", UserID: "user-a", SourceEventKey: "event-a", Type: "system", Title: "A", Body: "A"},
		{BusinessID: "business-a", UserID: "user-b", SourceEventKey: "event-b", Type: "system", Title: "B", Body: "B"},
		{BusinessID: "business-b", UserID: "user-a", SourceEventKey: "event-c", Type: "system", Title: "C", Body: "C"},
	} {
		if _, err := service.Ingest(context.Background(), input); err != nil {
			t.Fatalf("Ingest() error = %v", err)
		}
	}

	updated, err := service.MarkAllRead(context.Background(), "business-a", "user-a")
	if err != nil || updated != 1 {
		t.Fatalf("MarkAllRead() = %d, %v", updated, err)
	}
	otherBusiness, _ := service.List(context.Background(), "business-b", "user-a", 100)
	otherUser, _ := service.List(context.Background(), "business-a", "user-b", 100)
	if otherBusiness[0].ReadAt != nil || otherUser[0].ReadAt != nil {
		t.Fatal("read-all crossed tenant or user scope")
	}
}
