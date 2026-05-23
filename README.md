# ffreis-platform-awsfakes

Lightweight AWS SDK v2 middleware for testing. Plugs into any SDK v2 client
(S3, DynamoDB, IAM, STS, …) to:

- **Record** calls per operation, for assertion
- **Fail** the Nth call (or every call) of a specific operation
- **Stub** a canned response so tests don't need a live AWS endpoint

The advantage over hand-rolled interface-fakes: this approach plugs into the
SDK's **real middleware stack**, so retry, backoff, and credential resolution
are exercised in tests too. Hand-rolled service-interface fakes skip those
layers entirely and can hide bugs in the consumer's retry config.

## Install

```bash
go get github.com/FelipeFuhr/ffreis-platform-awsfakes
```

## Usage

```go
import (
    "context"
    "errors"
    "testing"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/service/dynamodb"

    "github.com/FelipeFuhr/ffreis-platform-awsfakes/awsfakes"
)

func TestIdempotentReconvergence(t *testing.T) {
    rec := &awsfakes.Recorder{}
    cfg := aws.Config{
        Region:      "us-east-1",
        Credentials: aws.AnonymousCredentials{},
    }
    cfg.APIOptions = append(cfg.APIOptions,
        awsfakes.RecordCalls(rec),
        awsfakes.FailAtCall("PutItem", 1, errors.New("transient throttle")),
        awsfakes.StubResponse("PutItem", &dynamodb.PutItemOutput{}),
    )
    client := dynamodb.NewFromConfig(cfg)

    // First run: fails on the throttle.
    _, err := client.PutItem(context.Background(), validInput())
    if err == nil {
        t.Fatal("expected throttle on first call")
    }
    // Second run: same operation, succeeds.
    _, err = client.PutItem(context.Background(), validInput())
    if err != nil {
        t.Fatalf("expected success on second call, got %v", err)
    }
    if got := rec.For("PutItem"); got != 2 {
        t.Errorf("PutItem count = %d, want 2", got)
    }
}
```

## Public API

| Name | Purpose |
|---|---|
| `Recorder{}` | Concurrent-safe counter struct |
| `Recorder.For(op)` | Read the count for a specific operation |
| `Recorder.Total()` | Sum across all operations |
| `RecordCalls(rec)` | Middleware option: increment counter on every call |
| `FailAtCall(op, n, err)` | Middleware option: return err on the Nth call to op |
| `FailEveryCall(op, err)` | Middleware option: return err on every call to op |
| `StubResponse(op, result)` | Middleware option: short-circuit op with canned result |

All middleware options return `func(*middleware.Stack) error` suitable for
appending to `aws.Config.APIOptions`.

## Tips

- **Retry mode**: the SDK retries failed calls by default. For deterministic
  "fail once" tests, set `cfg.Retryer` to one with `MaxAttempts: 1` (see
  `awsfakes/awsfakes_test.go` `newCfg`).
- **Validation runs first**: the SDK validates required input fields before
  any APIOption middleware. Provide valid `Key`, `Item`, etc. or your fault
  won't get a chance to fire.
- **StubResponse type**: pass the *exact* pointer type the operation returns
  (e.g., `*dynamodb.GetItemOutput`). The SDK type-asserts at the call site.

## Tests

```bash
make test
```

Runs with `-race -shuffle=on` per the workspace invariant.
