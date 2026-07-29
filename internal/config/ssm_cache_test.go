package config

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/ssm/types"
)

type fakeSSM struct {
	mu     sync.Mutex
	calls  [][]string
	values map[string]string
	err    error
}

func (f *fakeSSM) GetParameters(_ context.Context, input *ssm.GetParametersInput, _ ...func(*ssm.Options)) (*ssm.GetParametersOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, append([]string(nil), input.Names...))
	if f.err != nil {
		return nil, f.err
	}
	out := &ssm.GetParametersOutput{}
	for _, name := range input.Names {
		value, ok := f.values[name]
		if !ok {
			out.InvalidParameters = append(out.InvalidParameters, name)
			continue
		}
		out.Parameters = append(out.Parameters, types.Parameter{Name: aws.String(name), Value: aws.String(value)})
	}
	return out, nil
}

func TestSSMResolverBatchesAndCachesConfiguredParameters(t *testing.T) {
	values := make(map[string]string)
	names := make([]string, 0, 11)
	for i := range 11 {
		name := fmt.Sprintf("/app/config/%02d", i)
		names = append(names, name)
		values[name] = fmt.Sprintf("value-%02d", i)
	}
	client := &fakeSSM{values: values}
	resolver, err := NewSSMResolver(client, names, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	got, err := resolver.Get(context.Background(), names)
	if err != nil {
		t.Fatalf("get parameters: %v", err)
	}
	if len(got) != len(names) {
		t.Fatalf("expected %d parameters, got %d", len(names), len(got))
	}
	if _, err := resolver.Get(context.Background(), names); err != nil {
		t.Fatalf("cached get: %v", err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) != 2 {
		t.Fatalf("expected two batches for eleven parameters and no cache refetch, got %d", len(client.calls))
	}
	for _, call := range client.calls {
		if len(call) > 10 {
			t.Fatalf("SSM batch exceeded ten names: %d", len(call))
		}
	}
}

func TestSSMResolverRejectsUnknownAndSanitizesUpstreamErrors(t *testing.T) {
	client := &fakeSSM{
		values: map[string]string{"/app/config/host": "db.internal"},
		err:    errors.New("upstream included " + sentinelSecret),
	}
	resolver, err := NewSSMResolver(client, []string{"/app/config/host"}, time.Minute, time.Now)
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	if _, err := resolver.Get(context.Background(), []string{"/not/configured"}); !IsConfigurationError(err) {
		t.Fatalf("expected typed configuration error, got %T: %v", err, err)
	}
	_, err = resolver.Get(context.Background(), []string{"/app/config/host"})
	if err == nil {
		t.Fatal("expected upstream error")
	}
	if containsSecret(err.Error()) {
		t.Fatalf("error leaked secret material: %v", err)
	}
}

func TestSSMResolverRefreshesAfterTTLAndSerializesConcurrentReads(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	client := &fakeSSM{values: map[string]string{"/app/config/host": "db.internal"}}
	resolver, err := NewSSMResolver(client, []string{"/app/config/host"}, time.Minute, func() time.Time { return now })
	if err != nil {
		t.Fatalf("new resolver: %v", err)
	}

	if _, err := resolver.Get(context.Background(), []string{"/app/config/host"}); err != nil {
		t.Fatalf("first read: %v", err)
	}
	now = now.Add(61 * time.Second)

	const readers = 24
	var wg sync.WaitGroup
	errs := make(chan error, readers)
	for range readers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			values, err := resolver.Get(context.Background(), []string{"/app/config/host"})
			if err != nil {
				errs <- err
				return
			}
			if values["/app/config/host"] != "db.internal" {
				errs <- errors.New("unexpected parameter value")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.calls) != 2 {
		t.Fatalf("expected one initial and one TTL refresh call, got %d", len(client.calls))
	}
}
