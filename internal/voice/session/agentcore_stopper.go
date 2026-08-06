package session

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcore"
)

type AgentCoreRuntimeAPI interface {
	StopRuntimeSession(context.Context, *bedrockagentcore.StopRuntimeSessionInput, ...func(*bedrockagentcore.Options)) (*bedrockagentcore.StopRuntimeSessionOutput, error)
}

type AgentCoreRuntimeStopper struct {
	client AgentCoreRuntimeAPI
}

func NewAgentCoreRuntimeStopper(client AgentCoreRuntimeAPI) *AgentCoreRuntimeStopper {
	return &AgentCoreRuntimeStopper{client: client}
}

func (s *AgentCoreRuntimeStopper) StopRuntimeSession(ctx context.Context, target RuntimeTarget) error {
	if s == nil || s.client == nil {
		return ErrUnavailable
	}
	if target.AgentRuntimeARN == "" || target.RuntimeSessionID == "" || target.Qualifier == "" {
		return fmt.Errorf("invalid AgentCore stop target: %w", ErrInvalidRequest)
	}
	output, err := s.client.StopRuntimeSession(ctx, &bedrockagentcore.StopRuntimeSessionInput{
		AgentRuntimeArn: aws.String(target.AgentRuntimeARN), RuntimeSessionId: aws.String(target.RuntimeSessionID),
		Qualifier: aws.String(target.Qualifier), ClientToken: aws.String(transactionToken("stop", target.RuntimeSessionID)),
	})
	if err != nil {
		return err
	}
	if output == nil || output.StatusCode == nil || *output.StatusCode < 200 || *output.StatusCode >= 300 {
		return fmt.Errorf("AgentCore stop returned a non-success status")
	}
	return nil
}
