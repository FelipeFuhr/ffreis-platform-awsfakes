# Agent Context

**This repo:** `ffreis-platform-awsfakes` — AWS SDK v2 middleware for
testing. Used by downstream platform repos for fault injection, call
recording, and response stubbing in unit tests.

## Non-obvious facts

- **Middleware vs. interface fakes.** This library uses
  `smithy-go/middleware.InitializeMiddlewareFunc` so the SDK's real retry,
  backoff, and credential-resolution layers run in tests. Hand-rolled
  service-interface fakes (the older pattern in some downstream repos)
  bypass those layers — they're easier to write but hide retry-config bugs.
  When migrating a repo from interface fakes to this library, expect to
  discover incidental "tests passed but retry was broken" issues.

- **Input validation runs before APIOptions.** The SDK validates required
  fields in its own Initialize-stage middleware that runs BEFORE the
  caller's APIOptions. If your test passes an invalid input (missing
  TableName, Key, Item, etc.), the SDK returns a validation error and
  your fault never gets a chance to fire. Always pass valid inputs.

- **`-race -shuffle=on` is the test invariant.** The `Recorder` uses a
  sync.Mutex precisely because shuffle-enabled testing surfaces racy mocks
  fast. Don't drop the mutex.

- **Disable retry in tests** when you want deterministic single-shot
  failures: `cfg.Retryer = func() aws.Retryer { return retry.AddWithMaxAttempts(retry.NewStandard(), 1) }`.
  Otherwise a FailAtCall(op, 1, err) will retry to FailAtCall(op, 2) and
  succeed, masking the test.

## Structure

```
awsfakes/
  awsfakes.go         ← Recorder, RecordCalls, FailAtCall, FailEveryCall, StubResponse
  awsfakes_test.go    ← end-to-end tests using a real dynamodb.Client
Makefile
README.md
go.mod
```

## Test the lib

```bash
make test    # -race -shuffle=on
```

## Public repo — private-repo hygiene

This is a public GitHub repository. Never name private repos in commit
messages, PR titles, or descriptions. Use generic terms ("a private
consumer", "internal infra").

## Keeping this file current

- If you add a new middleware option (e.g. `Latency()` to simulate slow
  calls), document it in README's API table.
- If you discover a new "input validation runs before APIOptions"-style
  gotcha, add it to "Non-obvious facts" — these are easy to miss.
