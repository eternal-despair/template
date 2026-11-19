package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	tracer     trace.Tracer
}

type PushRequest struct {
	RequestID   string            `json:"request_id"`
	RecipientID uuid.UUID         `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Data        map[string]string `json:"data,omitempty"`
}

type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("push service responded with status %d: %s", e.StatusCode, e.Body)
}

func NewClient(baseURL string, timeout time.Duration, tracer trace.Tracer) *Client {
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 20,
		IdleConnTimeout:     90 * time.Second,
	}

	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   timeout,
		},
		tracer: tracer,
	}
}

func (c *Client) RequestPosition(ctx context.Context, driverID, tripID uuid.UUID) error {
	var span trace.Span
	if c.tracer != nil {
		ctx, span = c.tracer.Start(ctx, "push-service.request-position")
		defer span.End()
	}

	reqBody := PushRequest{
		RequestID:   uuid.New().String(),
		RecipientID: driverID,
		Kind:        "REQUEST_POSITION",
		Data: map[string]string{
			"trip_id": tripID.String(),
		},
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("marshal push request: %w", err)
	}

	reqURL := c.baseURL + "/api/v1/push"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(req.Header))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if span != nil {
			span.RecordError(err)
		}
		return fmt.Errorf("push service transport error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		httpErr := &HTTPError{
			StatusCode: resp.StatusCode,
			Body:       string(bodyBytes),
		}
		if span != nil {
			span.RecordError(httpErr)
		}
		return httpErr
	}

	return nil
}
