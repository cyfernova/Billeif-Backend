package nlp

import (
	"context"
	"fmt"
	"time"
)

// IdeaGenerator generates ideas for AI assistants
type IdeaGenerator struct {
	client  LLMClient
	timeout time.Duration
}

// NewIdeaGenerator creates a new idea generator
func NewIdeaGenerator(config *LLMConfig) (*IdeaGenerator, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("API key required for Idea Generator")
	}

	var client LLMClient
	var err error

	switch config.Provider {
	case "claude":
		client, err = NewClaudeClient(config.APIKey, config.Model)
	case "gemini":
		client, err = NewGeminiClient(config.APIKey, config.Model)
	default:
		return nil, fmt.Errorf("unsupported LLM provider: %s", config.Provider)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to create LLM client: %w", err)
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &IdeaGenerator{
		client:  client,
		timeout: timeout,
	}, nil
}

// GenerateIdeas generates ideas for AI assistants based on user input
func (g *IdeaGenerator) GenerateIdeas(ctx context.Context, userInput string) (string, error) {
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, g.timeout)
	defer cancel()

	// Build the system prompt
	prompt := buildIdeaGenerationPrompt(userInput)

	// Call the LLM
	return g.client.Call(ctx, prompt)
}

func buildIdeaGenerationPrompt(userInput string) string {
	return fmt.Sprintf(`# AI Assistant Idea Generator - Base Template

## Core Purpose
You are a specialized AI designed to help users ideate and conceptualize AI assistants and agents. Your primary goal is to generate creative, practical, and innovative ideas for AI solutions based on user requirements.

## Terminology
- **AI Assistant**: A system prompt applied to a large language model, potentially with modified parameters (especially temperature) and additional data pipelines through RAG or API access.
- **Agent**: An assistant with "tool" access, allowing it to take actions upon other APIs or systems through intermediary software.

## Response Format
For each assistant idea, provide:

1. **Name**: A creative but descriptive name
2. **Description**: A concise explanation of what the assistant does and its benefits
3. **Capabilities**: Required technical capabilities beyond a basic LLM (vision, RAG, APIs, etc.)
4. **Implementation Difficulty**: (Easy, Medium, Hard)

## Ideation Guidelines
- Focus on practical, valuable assistants that solve real problems
- Balance creativity with feasibility
- Include a mix of business and personal use cases unless specified otherwise
- Avoid repeating suggestions from the same session

## User Input
"%s"

## Task
Generate 3 distinct AI assistant/agent ideas based on the user input above. Return the result in Markdown format.`, userInput)
}
