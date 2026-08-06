package session

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcore"
	"github.com/aws/aws-sdk-go-v2/service/bedrockagentcore/types"
)

type fakeAgentCoreRuntime struct {
	input *bedrockagentcore.StopRuntimeSessionInput
	err   error
}

func (f *fakeAgentCoreRuntime) StopRuntimeSession(_ context.Context, input *bedrockagentcore.StopRuntimeSessionInput, _ ...func(*bedrockagentcore.Options)) (*bedrockagentcore.StopRuntimeSessionOutput, error) {
	f.input = input
	if f.err != nil {
		return nil, f.err
	}
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

func TestAgentCoreRuntimeStopperTreatsAlreadyAbsentSessionAsStopped(t *testing.T) {
	target := RuntimeTarget{
		AgentRuntimeARN:  "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/test",
		RuntimeSessionID: "voice-session-01K1ABCDE2FGHIJK3LMNOPQRST", Qualifier: "PROD",
	}

	client := &fakeAgentCoreRuntime{err: &types.ResourceNotFoundException{Message: aws.String("already terminated")}}
	if err := NewAgentCoreRuntimeStopper(client).StopRuntimeSession(context.Background(), target); err != nil {
		t.Fatalf("already absent runtime session must converge as stopped: %v", err)
	}

	want := errors.New("temporary provider failure")
	client.err = want
	if err := NewAgentCoreRuntimeStopper(client).StopRuntimeSession(context.Background(), target); !errors.Is(err, want) {
		t.Fatalf("non-terminal provider error = %v, want %v", err, want)
	}
}

func TestAgentCoreRuntimeStopperRejectsTypedNilClient(t *testing.T) {
	var client *fakeAgentCoreRuntime
	stopper := NewAgentCoreRuntimeStopper(client)
	err := stopper.StopRuntimeSession(context.Background(), RuntimeTarget{
		AgentRuntimeARN:  "arn:aws:bedrock-agentcore:ap-south-1:123456789012:runtime/test",
		RuntimeSessionID: "voice-session-01K1ABCDE2FGHIJK3LMNOPQRST", Qualifier: "PROD",
	})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("StopRuntimeSession(typed nil client) error = %v, want ErrUnavailable", err)
	}
}
