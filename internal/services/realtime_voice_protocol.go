package services

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/config"

	"github.com/gorilla/websocket"
)

const (
	AppEventReady                = "ready"
	AppEventSettingsApplied      = "settings_applied"
	AppEventConversationText     = "conversation_text"
	AppEventUserStartedSpeaking  = "user_started_speaking"
	AppEventAgentThinking        = "agent_thinking"
	AppEventAgentStartedSpeaking = "agent_started_speaking"
	AppEventAgentAudioDone       = "agent_audio_done"
	AppEventInterrupted          = "interrupted"
	AppEventPong                 = "pong"
	AppEventError                = "error"
)

type RealtimeVoiceControlEvent struct {
	Type            string                 `json:"type"`
	ConversationID  string                 `json:"conversation_id,omitempty"`
	BusinessID      string                 `json:"business_id,omitempty"`
	VisibleMessages []RealtimeHistoryItem  `json:"visible_messages,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

type RealtimeHistoryItem struct {
	Type    string `json:"type,omitempty"`
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type RealtimeAppOutbound struct {
	MessageType int
	Payload     []byte
}

type RealtimeDeepgramOutbound struct {
	MessageType int
	Payload     []byte
}

type RealtimeAppEvent struct {
	Type    string      `json:"type"`
	Role    string      `json:"role,omitempty"`
	Content string      `json:"content,omitempty"`
	Code    string      `json:"code,omitempty"`
	Message string      `json:"message,omitempty"`
	Data    interface{} `json:"data,omitempty"`
}

type DeepgramFunctionCallRequest struct {
	Type      string                 `json:"type"`
	Functions []DeepgramFunctionCall `json:"functions"`
}

type DeepgramFunctionCall struct {
	ID               string          `json:"id"`
	Name             string          `json:"name"`
	Arguments        json.RawMessage `json:"arguments"`
	ClientSide       bool            `json:"client_side"`
	ThoughtSignature string          `json:"thought_signature,omitempty"`
}

func ParseRealtimeVoiceControl(payload []byte) (RealtimeVoiceControlEvent, error) {
	var event RealtimeVoiceControlEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return event, fmt.Errorf("control message must be JSON")
	}

	event.Type = strings.TrimSpace(event.Type)
	switch event.Type {
	case "start", "stop", "interrupt", "ping", "client_context":
		return event, nil
	case "":
		return event, fmt.Errorf("control message type is required")
	default:
		return event, fmt.Errorf("unsupported control message type %q", event.Type)
	}
}

func NewAppJSONEvent(event RealtimeAppEvent) RealtimeAppOutbound {
	payload, _ := json.Marshal(event)
	return RealtimeAppOutbound{MessageType: websocket.TextMessage, Payload: payload}
}

func NewAppError(code, message string) RealtimeAppOutbound {
	return NewAppJSONEvent(RealtimeAppEvent{
		Type:    AppEventError,
		Code:    code,
		Message: message,
	})
}

func NewDeepgramJSONMessage(message interface{}) RealtimeDeepgramOutbound {
	payload, _ := json.Marshal(message)
	return RealtimeDeepgramOutbound{MessageType: websocket.TextMessage, Payload: payload}
}

func MapDeepgramJSONEvent(payload []byte) (RealtimeAppEvent, bool, error) {
	var envelope struct {
		Type        string          `json:"type"`
		Role        string          `json:"role"`
		Content     string          `json:"content"`
		Code        string          `json:"code"`
		Message     string          `json:"message"`
		Description string          `json:"description"`
		Reason      string          `json:"reason"`
		Raw         json.RawMessage `json:"-"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return RealtimeAppEvent{}, false, fmt.Errorf("invalid Deepgram JSON event: %w", err)
	}
	envelope.Raw = append(envelope.Raw[:0], payload...)

	switch envelope.Type {
	case "SettingsApplied":
		return RealtimeAppEvent{Type: AppEventSettingsApplied}, true, nil
	case "ConversationText":
		return RealtimeAppEvent{
			Type:    AppEventConversationText,
			Role:    envelope.Role,
			Content: envelope.Content,
		}, true, nil
	case "UserStartedSpeaking":
		return RealtimeAppEvent{Type: AppEventUserStartedSpeaking}, true, nil
	case "AgentThinking":
		return RealtimeAppEvent{Type: AppEventAgentThinking, Content: envelope.Content}, true, nil
	case "AgentStartedSpeaking":
		var raw map[string]interface{}
		_ = json.Unmarshal(payload, &raw)
		return RealtimeAppEvent{Type: AppEventAgentStartedSpeaking, Data: raw}, true, nil
	case "AgentAudioDone":
		return RealtimeAppEvent{Type: AppEventAgentAudioDone}, true, nil
	case "Error":
		code := strings.TrimSpace(envelope.Code)
		if code == "" {
			code = "deepgram_error"
		}
		message := strings.TrimSpace(envelope.Message)
		if message == "" {
			message = strings.TrimSpace(envelope.Description)
		}
		if message == "" {
			message = strings.TrimSpace(envelope.Reason)
		}
		if message == "" {
			message = "Deepgram voice agent returned an error"
		}
		return RealtimeAppEvent{Type: AppEventError, Code: code, Message: message}, true, nil
	case "Warning":
		code := strings.TrimSpace(envelope.Code)
		if code == "" {
			code = "deepgram_warning"
		}
		message := strings.TrimSpace(envelope.Message)
		if message == "" {
			message = strings.TrimSpace(envelope.Description)
		}
		if message == "" {
			message = strings.TrimSpace(envelope.Reason)
		}
		if message == "" {
			message = "Deepgram voice agent returned a warning"
		}
		return RealtimeAppEvent{Type: AppEventError, Code: code, Message: message}, true, nil
	case "FunctionCallRequest":
		var request DeepgramFunctionCallRequest
		if err := json.Unmarshal(payload, &request); err != nil {
			return RealtimeAppEvent{}, false, err
		}
		names := make([]string, 0, len(request.Functions))
		for _, fn := range request.Functions {
			names = append(names, fn.Name)
		}
		return RealtimeAppEvent{
			Type:    "function_call_request",
			Message: "Function calls are handled server-side.",
			Data: map[string]interface{}{
				"functions": names,
			},
		}, true, nil
	case "Welcome", "History", "PromptUpdated", "ThinkUpdated", "SpeakUpdated", "InjectionRefused":
		return RealtimeAppEvent{}, false, nil
	default:
		return RealtimeAppEvent{}, false, nil
	}
}

type DeepgramVoiceAgentSettingsOptions struct {
	SessionID       string
	UserID          string
	BusinessID      string
	ConversationID  string
	Language        string
	Voice           string
	History         []RealtimeHistoryItem
	MCPToolsEnabled bool
}

func BuildDeepgramVoiceAgentSettings(cfg config.VoiceRealtimeConfig, opts DeepgramVoiceAgentSettingsOptions) map[string]interface{} {
	language := strings.TrimSpace(opts.Language)
	if language == "" {
		language = "en"
	}
	speakModel := strings.TrimSpace(opts.Voice)
	if speakModel == "" {
		speakModel = cfg.SpeakModel
	}

	agent := map[string]interface{}{
		"language": language,
		"listen": map[string]interface{}{
			"provider": map[string]interface{}{
				"type":         "deepgram",
				"model":        cfg.ListenModel,
				"smart_format": false,
			},
		},
		"think": map[string]interface{}{
			"provider": map[string]interface{}{
				"type":        "open_ai",
				"model":       cfg.DeepSeekModel,
				"temperature": 0.7,
			},
			"endpoint": map[string]interface{}{
				"url": cfg.DeepSeekChatCompletionsURL(),
				"headers": map[string]interface{}{
					"authorization": "Bearer " + strings.TrimSpace(cfg.DeepSeekAPIKey),
				},
			},
			"prompt": buildRealtimeVoicePrompt(opts),
		},
		"speak": map[string]interface{}{
			"provider": map[string]interface{}{
				"type":  "deepgram",
				"model": speakModel,
			},
		},
	}
	if opts.MCPToolsEnabled {
		think := agent["think"].(map[string]interface{})
		think["functions"] = BuildVoiceMCPFunctionDefinitions()
	}

	if len(opts.History) > 0 {
		messages := make([]map[string]string, 0, len(opts.History))
		for _, item := range opts.History {
			role := strings.TrimSpace(item.Role)
			content := strings.TrimSpace(item.Content)
			if (role == "user" || role == "assistant") && content != "" {
				messages = append(messages, map[string]string{
					"type":    "History",
					"role":    role,
					"content": content,
				})
			}
		}
		if len(messages) > 0 {
			agent["context"] = map[string]interface{}{"messages": messages}
		}
	}

	return map[string]interface{}{
		"type": "Settings",
		"tags": []string{"billeif", "voice_realtime"},
		"audio": map[string]interface{}{
			"input": map[string]interface{}{
				"encoding":    cfg.InputEncoding,
				"sample_rate": cfg.InputSampleRate,
			},
			"output": map[string]interface{}{
				"encoding":    cfg.OutputEncoding,
				"sample_rate": cfg.OutputSampleRate,
				"container":   "none",
			},
		},
		"agent":       agent,
		"mip_opt_out": false,
		"flags": map[string]interface{}{
			"history": true,
		},
	}
}

func buildRealtimeVoicePrompt(opts DeepgramVoiceAgentSettingsOptions) string {
	prompt := "You are a fast, concise realtime voice assistant for the Billeif app. Keep replies short, natural, and useful for small-business finance workflows. Do not mention internal tooling or provider details."
	if opts.BusinessID != "" {
		prompt += "\nBusiness context is already authenticated server-side for business_id " + opts.BusinessID + "."
	}
	if opts.ConversationID != "" {
		prompt += "\nContinue the voice conversation identified by conversation_id " + opts.ConversationID + "."
	}
	if opts.MCPToolsEnabled {
		prompt += "\nWhen the user asks you to list, view, check, summarize, create, update, record, or adjust ordinary business finance data, use the available function to run an allowlisted Billeif MCP action. Only call write actions when the user clearly asks for the change and provides the required details. Do not delete data, authenticate users, administer accounts, send invoices, or call auth, credential, admin, destructive, or unrelated tools."
	}
	prompt += "\nCurrent session started at " + time.Now().UTC().Format(time.RFC3339) + "."
	return prompt
}
