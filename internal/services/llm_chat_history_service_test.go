package services

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func newLLMChatHistoryTestService(t *testing.T) (*LLMChatHistoryService, *gorm.DB, string, string) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	createLLMChatHistoryTestSchema(t, db)
	return NewLLMChatHistoryService(db, logger.New()), db, uuid.NewString(), uuid.NewString()
}

func createLLMChatHistoryTestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE llm_chat_conversations (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			title TEXT NOT NULL,
			last_message TEXT NOT NULL DEFAULT '',
			message_count INTEGER NOT NULL DEFAULT 0,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE llm_chat_messages (
			id TEXT PRIMARY KEY,
			conversation_id TEXT NOT NULL,
			business_id TEXT NOT NULL,
			user_id TEXT NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			web_search TEXT,
			created_at DATETIME
		)`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("create llm chat history schema: %v", err)
		}
	}
}

func TestLLMChatHistoryServiceSaveExchangeCreatesConversationAndMessages(t *testing.T) {
	svc, _, businessID, userID := newLLMChatHistoryTestService(t)
	ctx := context.Background()
	result := &LLMChatResult{
		Response: "As of 20 May 2026, GST e-invoice rules still apply.",
		WebSearch: &LLMWebSearchState{
			Used:  true,
			Query: "latest GST e-invoice India update",
			Results: []LLMSearchResult{{
				Title: "GST e-invoice update",
				URL:   "https://example.com/gst",
			}},
		},
	}

	conversation, messages, err := svc.SaveExchange(ctx, SaveLLMChatExchangeInput{
		BusinessID: businessID,
		UserID:     userID,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are Billeif AI."},
			{Role: "user", Content: "Search the web for latest GST e-invoice India update today"},
		},
		Result: result,
	})
	if err != nil {
		t.Fatalf("save exchange: %v", err)
	}
	if conversation.ID == "" {
		t.Fatal("expected conversation id")
	}
	if result.ConversationID != conversation.ID {
		t.Fatalf("expected result conversation id %q, got %q", conversation.ID, result.ConversationID)
	}
	if conversation.Title != "Search the web for latest GST e-invoice India update today" {
		t.Fatalf("unexpected title: %q", conversation.Title)
	}
	if conversation.LastMessage != result.Response {
		t.Fatalf("expected last message to be assistant response, got %q", conversation.LastMessage)
	}
	if conversation.MessageCount != 2 {
		t.Fatalf("expected message count 2, got %d", conversation.MessageCount)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 saved messages, got %d", len(messages))
	}
	if messages[0].Role != "user" || messages[1].Role != "assistant" {
		t.Fatalf("unexpected saved roles: %s, %s", messages[0].Role, messages[1].Role)
	}

	history, err := svc.ListMessages(ctx, businessID, userID, conversation.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(history) != 2 {
		t.Fatalf("expected 2 history messages, got %d", len(history))
	}
	if history[1].WebSearch == nil || !history[1].WebSearch.Used {
		raw, _ := json.Marshal(history[1].WebSearch)
		t.Fatalf("expected assistant web search payload, got %s", raw)
	}
}

func TestChatHistoryOrdersUserBeforeReplyWithSameTimestamp(t *testing.T) {
	svc, db, businessID, userID := newLLMChatHistoryTestService(t)
	conversationID := uuid.NewString()
	conversation := models.LLMChatConversation{ID: conversationID, BusinessID: businessID, UserID: userID, Title: "Greeting"}
	if err := db.Create(&conversation).Error; err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// A query plan may encounter the reply first when an exchange shares a timestamp.
	for _, role := range []string{"assistant", "user"} {
		message := models.LLMChatMessageRecord{ID: uuid.NewString(), ConversationID: conversationID, BusinessID: businessID, UserID: userID, Role: role, Content: role, CreatedAt: now}
		if err := db.Create(&message).Error; err != nil {
			t.Fatal(err)
		}
	}
	history, err := svc.ListMessages(context.Background(), businessID, userID, conversationID)
	if err != nil || len(history) != 2 || history[0].Role != "user" || history[1].Role != "assistant" {
		t.Fatalf("history order = %#v, error = %v", history, err)
	}
}

func TestLLMChatHistoryServiceAppendsToExistingConversation(t *testing.T) {
	svc, _, businessID, userID := newLLMChatHistoryTestService(t)
	ctx := context.Background()

	first, _, err := svc.SaveExchange(ctx, SaveLLMChatExchangeInput{
		BusinessID: businessID,
		UserID:     userID,
		Messages:   []ChatMessage{{Role: "user", Content: "Who is Mira Murati?"}},
		Result:     &LLMChatResult{Response: "Mira Murati is an AI executive.", WebSearch: &LLMWebSearchState{Used: true}},
	})
	if err != nil {
		t.Fatalf("save first exchange: %v", err)
	}

	secondResult := &LLMChatResult{Response: "Thinking Machines Lab was announced in 2025.", WebSearch: &LLMWebSearchState{Used: true}}
	second, _, err := svc.SaveExchange(ctx, SaveLLMChatExchangeInput{
		BusinessID:     businessID,
		UserID:         userID,
		ConversationID: first.ID,
		Messages:       []ChatMessage{{Role: "user", Content: "What did she start after OpenAI?"}},
		Result:         secondResult,
	})
	if err != nil {
		t.Fatalf("save second exchange: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("expected same conversation id, got %q and %q", first.ID, second.ID)
	}
	if second.MessageCount != 4 {
		t.Fatalf("expected message count 4, got %d", second.MessageCount)
	}

	conversations, err := svc.ListConversations(ctx, businessID, userID, 20)
	if err != nil {
		t.Fatalf("list conversations: %v", err)
	}
	if len(conversations) != 1 {
		t.Fatalf("expected 1 conversation, got %d", len(conversations))
	}
	if conversations[0].LastMessage != secondResult.Response {
		t.Fatalf("expected latest response in conversation list, got %q", conversations[0].LastMessage)
	}

	messages, err := svc.ListMessages(ctx, businessID, userID, first.ID)
	if err != nil {
		t.Fatalf("list messages: %v", err)
	}
	if len(messages) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(messages))
	}
}
