// Package awsfakes provides AWS SDK v2 middleware for testing. It plugs
// into any SDK v2 client (S3, DynamoDB, IAM, STS, …) and:
//
//   - Returns a canned response for a specific operation, OR
//   - Injects an error at the Nth call to a specific operation, OR
//   - Counts calls per operation for assertion.
//
// The advantage over hand-rolled interface-fakes: this approach plugs into
// the SDK's real middleware stack, so retry, backoff, and credential
// resolution are exercised in tests too. Hand-rolled service-interface
// fakes skip those layers entirely and can hide bugs in the consumer's
// retry/backoff configuration.
//
// Basic usage:
//
//	cfg := aws.Config{Region: "us-east-1"}
//	calls := &awsfakes.Recorder{}
//	cfg.APIOptions = append(cfg.APIOptions,
//	    awsfakes.RecordCalls(calls),
//	    awsfakes.FailAtCall("GetItem", 1, errors.New("throttle")),
//	    awsfakes.StubResponse("PutItem", &dynamodb.PutItemOutput{}),
//	)
//	client := dynamodb.NewFromConfig(cfg)
//	_, err := client.PutItem(ctx, &dynamodb.PutItemInput{...})
//	// err is nil; calls.For("PutItem") == 1.
package awsfakes

import (
	"context"
	"errors"
	"fmt"
	"sync"

	"github.com/aws/smithy-go/middleware"
)

// Recorder counts calls per operation name. Safe for concurrent use; client
// SDKs commonly issue calls from goroutines (retry, presigner, etc.).
type Recorder struct {
	mu     sync.Mutex
	counts map[string]int
}

// For returns the number of times operation has been invoked.
func (r *Recorder) For(operation string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.counts[operation]
}

// Total returns the sum of calls across all operations.
func (r *Recorder) Total() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, n := range r.counts {
		total += n
	}
	return total
}

func (r *Recorder) inc(operation string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.counts == nil {
		r.counts = make(map[string]int)
	}
	r.counts[operation]++
	return r.counts[operation]
}

// RecordCalls returns an APIOption that increments the per-operation
// counter on every Initialize phase entry. Apply by appending to
// `cfg.APIOptions`.
func RecordCalls(rec *Recorder) func(*middleware.Stack) error {
	return func(stack *middleware.Stack) error {
		return stack.Initialize.Add(middleware.InitializeMiddlewareFunc(
			"awsfakes.RecordCalls",
			func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
				rec.inc(operationName(ctx))
				return next.HandleInitialize(ctx, in)
			},
		), middleware.After)
	}
}

// FailAtCall makes the Nth invocation of operation return errToReturn,
// short-circuiting the rest of the middleware stack. The 1st call is N=1.
// Subsequent calls proceed normally. Use multiple FailAtCall options to
// chain failures.
func FailAtCall(operation string, n int, errToReturn error) func(*middleware.Stack) error {
	if errToReturn == nil {
		errToReturn = errors.New("awsfakes: synthetic failure")
	}
	var count int
	var mu sync.Mutex
	return func(stack *middleware.Stack) error {
		return stack.Initialize.Add(middleware.InitializeMiddlewareFunc(
			fmt.Sprintf("awsfakes.FailAtCall:%s:%d", operation, n),
			func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
				if operationName(ctx) != operation {
					return next.HandleInitialize(ctx, in)
				}
				mu.Lock()
				count++
				match := count == n
				mu.Unlock()
				if match {
					return middleware.InitializeOutput{}, middleware.Metadata{}, errToReturn
				}
				return next.HandleInitialize(ctx, in)
			},
		), middleware.After)
	}
}

// FailEveryCall makes every invocation of operation return errToReturn.
// Useful for exhaustively driving the error path of a consumer.
func FailEveryCall(operation string, errToReturn error) func(*middleware.Stack) error {
	if errToReturn == nil {
		errToReturn = errors.New("awsfakes: synthetic failure")
	}
	return func(stack *middleware.Stack) error {
		return stack.Initialize.Add(middleware.InitializeMiddlewareFunc(
			fmt.Sprintf("awsfakes.FailEveryCall:%s", operation),
			func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
				if operationName(ctx) == operation {
					return middleware.InitializeOutput{}, middleware.Metadata{}, errToReturn
				}
				return next.HandleInitialize(ctx, in)
			},
		), middleware.After)
	}
}

// StubResponse returns a fixed response for operation, short-circuiting the
// real call so the consumer does not need a live AWS endpoint. The Result
// must be the pointer type the operation returns (e.g., *dynamodb.GetItemOutput).
// Unknown operations pass through to the next middleware.
func StubResponse(operation string, result any) func(*middleware.Stack) error {
	return func(stack *middleware.Stack) error {
		return stack.Initialize.Add(middleware.InitializeMiddlewareFunc(
			fmt.Sprintf("awsfakes.StubResponse:%s", operation),
			func(ctx context.Context, in middleware.InitializeInput, next middleware.InitializeHandler) (middleware.InitializeOutput, middleware.Metadata, error) {
				if operationName(ctx) != operation {
					return next.HandleInitialize(ctx, in)
				}
				return middleware.InitializeOutput{Result: result}, middleware.Metadata{}, nil
			},
		), middleware.After)
	}
}

// operationName extracts the operation name from the smithy-go context. The
// SDK populates this on every API call.
func operationName(ctx context.Context) string {
	return middleware.GetOperationName(ctx)
}
