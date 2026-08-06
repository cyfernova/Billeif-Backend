package session

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcore"
)

type fakeAgentCoreRuntime struct {
	input *bedrockagentcore.StopRuntimeSessionInput
}

func (f *fakeAgentCoreRuntime) StopRuntimeSession(_ context.Context, input *bedrockagentcore.StopRuntimeSessionInput, _ ...func(*bedrockagentcore.Options)) (*bedrockagentcore.StopRuntimeSessionOutput, error) {
	f.input = input
	return &bedrockagentcore.StopRuntimeSessionOutput{RuntimeSessionId: input.RuntimeSessionId, StatusCode: aws.Int32(200)}, nil
}

func TestAgentCoreRuntimeStopperUsesExactRuntimeTarget(t *testing.T) {
	client := &fakeAgentCoreRuntime{}
	stopper := NewAgentCoreRuntimeStopper(client)
	target := RuntimeTarget{
		AgentRuntimeARN:  "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/test",
		RuntimeSessionID: "voice-session-01K1ABCDE2FGHIJK3LMNOPQRST", Qualifier: "PROD",
	}
	if err := stopper.StopRuntimeSession(context.Background(), target); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if aws.ToString(client.input.AgentRuntimeArn) != target.AgentRuntimeARN || aws.ToString(client.input.RuntimeSessionId) != target.RuntimeSessionID || aws.ToString(client.input.Qualifier) != target.Qualifier {
		t.Fatalf("stop target changed: %#v", client.input)
	}
	if token := aws.ToString(client.input.ClientToken); len(token) < 1 || len(token) > 36 {
		t.Fatalf("invalid idempotency token %q", token)
	}
}
