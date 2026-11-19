package push_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/eternal-despair/template/internal/gateway/push"
)

func TestClient_RequestPosition_Success(t *testing.T) {
	driverID := uuid.New()
	tripID := uuid.New()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		assert.Equal(t, "/api/v1/push", r.URL.Path)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"message_id":"123","accepted_at":"2026-08-24T12:00:00Z"}`))
	}))
	defer server.Close()

	client := push.NewClient(server.URL, 1*time.Second, nil)
	err := client.RequestPosition(context.Background(), driverID, tripID)
	require.NoError(t, err)
}

func TestClient_RequestPosition_ErrorStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"internal_error"}`))
	}))
	defer server.Close()

	client := push.NewClient(server.URL, 1*time.Second, nil)
	err := client.RequestPosition(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)

	var httpErr *push.HTTPError
	require.ErrorAs(t, err, &httpErr)
	assert.Equal(t, http.StatusInternalServerError, httpErr.StatusCode)
}

func TestClient_RequestPosition_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()

	client := push.NewClient(server.URL, 10*time.Millisecond, nil)
	err := client.RequestPosition(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
}
