package googlegenerativeai

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/alfredxw/denova/agent/model/providers"
	"google.golang.org/genai"
)

// The SDK discards error response headers. Retain retry and request identity
// at the transport boundary so Native owns the only retry policy.
type errorTransport struct{ next http.RoundTripper }

func (transport errorTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.next.RoundTrip(request)
	if err != nil || response.StatusCode < 400 {
		return response, err
	}
	defer response.Body.Close()
	data, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	var envelope struct{ Error genai.APIError }
	_ = json.Unmarshal(data, &envelope)
	message := strings.TrimSpace(envelope.Error.Message)
	if message == "" {
		message = http.StatusText(response.StatusCode)
	}
	requestID := response.Header.Get("x-goog-request-id")
	if requestID == "" {
		requestID = response.Header.Get("x-request-id")
	}
	return nil, &providers.APIError{
		StatusCode: response.StatusCode, Kind: envelope.Error.Status,
		Message: message, RequestID: requestID,
		RetryAfter: providers.RetryAfterDelay(response), Cause: envelope.Error,
	}
}

func adaptAPIError(err error) error {
	if err == nil {
		return nil
	}
	var providerError *providers.APIError
	if errors.As(err, &providerError) {
		return err
	}
	var sdkError genai.APIError
	if !errors.As(err, &sdkError) {
		return err
	}
	return &providers.APIError{
		StatusCode: sdkError.Code,
		Message:    strings.TrimSpace(sdkError.Message),
		Cause:      err,
	}
}
