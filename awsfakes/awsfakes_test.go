package awsfakes_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/aws/retry"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/FelipeFuhr/ffreis-platform-awsfakes/awsfakes"
)

// newCfg builds a minimal aws.Config suitable for unit testing — no real
// credentials needed, no network calls. The retry mode is disabled so a
// single FailAtCall produces a single visible failure (not 3 due to retries).
func newCfg() aws.Config {
	return aws.Config{
		Region:      "us-east-1",
		Credentials: aws.AnonymousCredentials{},
		RetryMode:   aws.RetryModeStandard,
		Retryer: func() aws.Retryer {
			return retry.AddWithMaxAttempts(retry.NewStandard(), 1)
		},
	}
}

// TestRecorder_CountsPerOperation verifies that RecordCalls increments
// the right counter when a real SDK client issues operations against the
// stubbed responses.
func TestRecorderCountsPerOperation(t *testing.T) {
	rec := &awsfakes.Recorder{}
	cfg := newCfg()
	cfg.APIOptions = append(cfg.APIOptions,
		awsfakes.RecordCalls(rec),
		awsfakes.StubResponse("PutItem", &dynamodb.PutItemOutput{}),
		awsfakes.StubResponse("GetItem", &dynamodb.GetItemOutput{Item: map[string]types.AttributeValue{}}),
	)
	client := dynamodb.NewFromConfig(cfg)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: "k"}}}); err != nil {
			t.Fatalf("PutItem #%d: %v", i, err)
		}
	}
	if _, err := client.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String("t"), Key: map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: "k"}}}); err != nil {
		t.Fatalf("GetItem: %v", err)
	}

	if got := rec.For("PutItem"); got != 3 {
		t.Errorf("PutItem count = %d, want 3", got)
	}
	if got := rec.For("GetItem"); got != 1 {
		t.Errorf("GetItem count = %d, want 1", got)
	}
	if got := rec.Total(); got != 4 {
		t.Errorf("Total = %d, want 4", got)
	}
}

// TestFailAtCall_OnlyFailsTheNthCall verifies the precise N-th-call
// failure injection, which is what idempotency / reconvergence tests rely
// on — fail once, then succeed.
func TestFailAtCallOnlyFailsTheNthCall(t *testing.T) {
	cfg := newCfg()
	sentinel := errors.New("synthetic throttle")
	cfg.APIOptions = append(cfg.APIOptions,
		awsfakes.FailAtCall("PutItem", 2, sentinel),
		awsfakes.StubResponse("PutItem", &dynamodb.PutItemOutput{}),
	)
	client := dynamodb.NewFromConfig(cfg)
	ctx := context.Background()

	// Call 1: succeeds.
	if _, err := client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: "k"}}}); err != nil {
		t.Fatalf("call 1: unexpected error %v", err)
	}
	// Call 2: fails with our sentinel.
	_, err := client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: "k"}}})
	if err == nil {
		t.Fatal("call 2: expected error, got nil")
	}
	if !errors.Is(err, sentinel) && !strings.Contains(err.Error(), "synthetic throttle") {
		t.Errorf("call 2: err = %v, expected to wrap or include sentinel", err)
	}
	// Call 3: succeeds (failure was 1-shot).
	if _, err := client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: "k"}}}); err != nil {
		t.Errorf("call 3: unexpected error %v", err)
	}
}

// TestFailEveryCall_OnlyAffectsTargetedOperation verifies the operation
// filter — a fault injected for PutItem must NOT affect GetItem.
func TestFailEveryCallOnlyAffectsTargetedOperation(t *testing.T) {
	cfg := newCfg()
	cfg.APIOptions = append(cfg.APIOptions,
		awsfakes.FailEveryCall("PutItem", errors.New("put-only failure")),
		awsfakes.StubResponse("PutItem", &dynamodb.PutItemOutput{}),
		awsfakes.StubResponse("GetItem", &dynamodb.GetItemOutput{}),
	)
	client := dynamodb.NewFromConfig(cfg)
	ctx := context.Background()

	if _, err := client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: "k"}}}); err == nil {
		t.Error("PutItem: expected failure, got nil")
	}
	if _, err := client.GetItem(ctx, &dynamodb.GetItemInput{TableName: aws.String("t"), Key: map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: "k"}}}); err != nil {
		t.Errorf("GetItem: unexpected failure %v", err)
	}
}

// TestStubResponse_ReturnedToCaller verifies that the result type set in
// StubResponse is actually surfaced to the caller (not just type-erased to
// the empty middleware.InitializeOutput).
func TestStubResponseReturnedToCaller(t *testing.T) {
	expected := &dynamodb.GetItemOutput{
		Item: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: "stubbed-pk"},
		},
	}
	cfg := newCfg()
	cfg.APIOptions = append(cfg.APIOptions, awsfakes.StubResponse("GetItem", expected))

	out, err := dynamodb.NewFromConfig(cfg).GetItem(context.Background(), &dynamodb.GetItemInput{TableName: aws.String("t"), Key: map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: "k"}}})
	if err != nil {
		t.Fatalf("GetItem: %v", err)
	}
	if out == nil {
		t.Fatal("GetItem returned nil output")
	}
	pk, ok := out.Item["PK"].(*types.AttributeValueMemberS)
	if !ok || pk.Value != "stubbed-pk" {
		t.Errorf("stubbed response not returned to caller; got Item=%v", out.Item)
	}
}

// TestComposition_RecorderAndFault verifies that RecordCalls runs even when
// FailAtCall short-circuits the stack. Tests rely on this to count failed
// calls.
func TestCompositionRecorderAndFault(t *testing.T) {
	rec := &awsfakes.Recorder{}
	cfg := newCfg()
	cfg.APIOptions = append(cfg.APIOptions,
		awsfakes.RecordCalls(rec),
		awsfakes.FailEveryCall("PutItem", errors.New("nope")),
	)
	client := dynamodb.NewFromConfig(cfg)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, _ = client.PutItem(ctx, &dynamodb.PutItemInput{TableName: aws.String("t"), Item: map[string]types.AttributeValue{"PK": &types.AttributeValueMemberS{Value: "k"}}})
	}
	if got := rec.For("PutItem"); got != 3 {
		t.Errorf("PutItem count = %d, want 3 (recorder must count failed calls too)", got)
	}
}
