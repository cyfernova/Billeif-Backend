package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	llmChatTitleMaxRunes = 72
	llmChatListLimit     = 50
)

type LLMChatHistoryService struct {
	db  *gorm.DB
	log *logger.Logger
}

type SaveLLMChatExchangeInput struct {
	BusinessID     string
	UserID         string
	ConversationID string
	Messages       []ChatMessage
	Result         *LLMChatResult
}

type LLMChatHistoryMessage struct {
	ID             string             `json:"id"`
	ConversationID string             `json:"conversation_id"`
	Role           string             `json:"role"`
	Content        string             `json:"content"`
	WebSearch      *LLMWebSearchState `json:"web_search,omitempty"`
	CreatedAt      string             `json:"created_at"`
}

func NewLLMChatHistoryService(db *gorm.DB, log *logger.Logger) *LLMChatHistoryService {
	return &LLMChatHistoryService{db: db, log: log}
}

func (s *LLMChatHistoryService) SaveExchange(ctx context.Context, input SaveLLMChatExchangeInput) (*models.LLMChatConversation, []models.LLMChatMessageRecord, error) {
	if s == nil || s.db == nil {
		return nil, nil, fmt.Errorf("chat history service unavailable")
	}
	businessID := strings.TrimSpace(input.BusinessID)
	userID := strings.TrimSpace(input.UserID)
	if businessID == "" || userID == "" {
		return nil, nil, fmt.Errorf("business and user scope are required")
	}
	if input.Result == nil {
		return nil, nil, fmt.Errorf("chat result is required")
	}
	userText := strings.TrimSpace(lastUserMessage(input.Messages))
	if userText == "" {
		return nil, nil, fmt.Errorf("user message is required")
	}
	assistantText := strings.TrimSpace(input.Result.Response)
	if assistantText == "" {
		return nil, nil, fmt.Errorf("assistant response is required")
	}

	var conversation models.LLMChatConversation
	var saved []models.LLMChatMessageRecord
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		conversationID := strings.TrimSpace(input.ConversationID)
		if conversationID == "" {
			conversation = models.LLMChatConversation{
				ID:           uuid.NewString(),
				BusinessID:   businessID,
				UserID:       userID,
				Title:        titleFromUserMessage(userText),
				LastMessage:  "",
				MessageCount: 0,
			}
			if err := tx.Create(&conversation).Error; err != nil {
				return fmt.Errorf("create chat conversation: %w", err)
			}
		} else {
			if err := tx.Where("id = ? AND business_id = ? AND user_id = ?", conversationID, businessID, userID).First(&conversation).Error; err != nil {
				if err == gorm.ErrRecordNotFound {
					return fmt.Errorf("chat conversation not found")
				}
				return fmt.Errorf("load chat conversation: %w", err)
			}
		}

		assistantWebSearch, err := marshalOptionalWebSearch(input.Result.WebSearch)
		if err != nil {
			return err
		}
		saved = []models.LLMChatMessageRecord{
			{
				ID:             uuid.NewString(),
				ConversationID: conversation.ID,
				BusinessID:     businessID,
				UserID:         userID,
				Role:           "user",
				Content:        userText,
			},
			{
				ID:             uuid.NewString(),
				ConversationID: conversation.ID,
				BusinessID:     businessID,
				UserID:         userID,
				Role:           "assistant",
				Content:        assistantText,
				WebSearchRaw:   assistantWebSearch,
			},
		}
		if err := tx.Create(&saved).Error; err != nil {
			return fmt.Errorf("create chat messages: %w", err)
		}

		conversation.LastMessage = assistantText
		conversation.MessageCount += len(saved)
		if err := tx.Save(&conversation).Error; err != nil {
			return fmt.Errorf("update chat conversation: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	input.Result.ConversationID = conversation.ID
	return &conversation, saved, nil
}

func (s *LLMChatHistoryService) ListConversations(ctx context.Context, businessID, userID string, limit int) ([]models.LLMChatConversation, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("chat history service unavailable")
	}
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(userID) == "" {
		return nil, fmt.Errorf("business and user scope are required")
	}
	if limit <= 0 || limit > llmChatListLimit {
		limit = llmChatListLimit
	}
	var conversations []models.LLMChatConversation
	err := s.db.WithContext(ctx).
		Where("business_id = ? AND user_id = ?", businessID, userID).
		Order("updated_at DESC").
		Limit(limit).
		Find(&conversations).Error
	return conversations, err
}

func (s *LLMChatHistoryService) ListMessages(ctx context.Context, businessID, userID, conversationID string) ([]LLMChatHistoryMessage, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("chat history service unavailable")
	}
	if strings.TrimSpace(businessID) == "" || strings.TrimSpace(userID) == "" || strings.TrimSpace(conversationID) == "" {
		return nil, fmt.Errorf("business, user, and conversation scope are required")
	}

	var count int64
	if err := s.db.WithContext(ctx).Model(&models.LLMChatConversation{}).
		Where("id = ? AND business_id = ? AND user_id = ?", conversationID, businessID, userID).
		Count(&count).Error; err != nil {
		return nil, fmt.Errorf("check chat conversation: %w", err)
	}
	if count == 0 {
		return nil, fmt.Errorf("chat conversation not found")
	}

	var records []models.LLMChatMessageRecord
	if err := s.db.WithContext(ctx).
		Where("conversation_id = ? AND business_id = ? AND user_id = ?", conversationID, businessID, userID).
		Order("created_at ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}
	messages := make([]LLMChatHistoryMessage, 0, len(records))
	for _, record := range records {
		message, err := historyMessageFromRecord(record)
		if err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, nil
}

func marshalOptionalWebSearch(state *LLMWebSearchState) (json.RawMessage, error) {
	if state == nil {
		return nil, nil
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("marshal web search payload: %w", err)
	}
	return raw, nil
}

func historyMessageFromRecord(record models.LLMChatMessageRecord) (LLMChatHistoryMessage, error) {
	message := LLMChatHistoryMessage{
		ID:             record.ID,
		ConversationID: record.ConversationID,
		Role:           record.Role,
		Content:        record.Content,
		CreatedAt:      record.CreatedAt.UTC().Format("2006-01-02T15:04:05.000Z"),
	}
	if len(record.WebSearchRaw) > 0 && string(record.WebSearchRaw) != "null" {
		var state LLMWebSearchState
		if err := json.Unmarshal(record.WebSearchRaw, &state); err != nil {
			return message, fmt.Errorf("decode web search payload: %w", err)
		}
		message.WebSearch = &state
	}
	return message, nil
}

func titleFromUserMessage(message string) string {
	title := strings.Join(strings.Fields(message), " ")
	runes := []rune(title)
	if len(runes) <= llmChatTitleMaxRunes {
		return title
	}
	return strings.TrimSpace(string(runes[:llmChatTitleMaxRunes-3])) + "..."
}
