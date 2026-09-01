package agentgateway

import (
	"context"
	"errors"
	"testing"

	"cc-filter/internal/agentgrant"
)

type fakeConsumer struct {
	calls int
	err   error
}

func (c *fakeConsumer) Consume(_ context.Context, _ agentgrant.ConsumeRequest) (agentgrant.ConsumeResponse, error) {
	c.calls++
	return agentgrant.ConsumeResponse{Consumed: c.err == nil, GrantID: "ag_once"}, c.err
}

type fakeBackend struct {
	calls              int
	lastIdempotencyKey string
}

func (b *fakeBackend) Execute(_ context.Context, _, _ string, _ any, idempotencyKey string) (any, error) {
	b.calls++
	b.lastIdempotencyKey = idempotencyKey
	if idempotencyKey == "" {
		return nil, errors.New("missing idempotency key")
	}
	return map[string]any{"deployed": true}, nil
}

func TestGatewayConsumesBeforeBackendAndStripsTransport(t *testing.T) {
	consumer, backend := &fakeConsumer{}, &fakeBackend{}
	gateway := New(consumer, backend)
	input := testInput()
	if _, err := gateway.Execute(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if consumer.calls != 1 || backend.calls != 1 {
		t.Fatalf("calls consumer=%d backend=%d", consumer.calls, backend.calls)
	}
}

func TestGatewayRejectsTamperingBeforeConsumption(t *testing.T) {
	consumer, backend := &fakeConsumer{}, &fakeBackend{}
	gateway := New(consumer, backend)
	input := testInput()
	input.Body = map[string]any{"release": "changed"}
	if _, err := gateway.Execute(context.Background(), input); err == nil {
		t.Fatal("tampered request was accepted")
	}
	if consumer.calls != 0 || backend.calls != 0 {
		t.Fatal("tampered request reached a downstream component")
	}
}

func TestGatewayDoesNotCallBackendWhenConsumptionFails(t *testing.T) {
	consumer, backend := &fakeConsumer{err: errors.New("replay")}, &fakeBackend{}
	if _, err := New(consumer, backend).Execute(context.Background(), testInput()); err == nil {
		t.Fatal("failed consumption was accepted")
	}
	if backend.calls != 0 {
		t.Fatal("backend was called after failed consumption")
	}
}

func testInput() Input {
	body := map[string]any{"release": "2026.08"}
	operation := testGatewayOperation(body)
	return Input{Method: "POST", URL: "https://api.staging.company.example/orders/deploy", Body: body, Grant: "opaque", Operation: operation}
}
