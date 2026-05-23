package llm

import "context"

// Mock is an in-memory Client for tests. Set Response or Err (or both) to
// control behaviour; Calls records every Request received.
type Mock struct {
	// Resp is what Complete returns on success. If nil, a default empty
	// response is returned.
	Resp *Response

	// Err, if set, is returned from Complete instead of Resp.
	Err error

	// ProviderName overrides the provider string. Defaults to "mock".
	ProviderName string

	// ModelName overrides the model string. Defaults to "mock-model".
	ModelName string

	// Calls captures every Request the test made. Length tells you how
	// often the agent invoked the client.
	Calls []Request
}

// Provider implements Client.
func (m *Mock) Provider() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return "mock"
}

// Model implements Client.
func (m *Mock) Model() string {
	if m.ModelName != "" {
		return m.ModelName
	}
	return "mock-model"
}

// Complete implements Client. Records the request, then returns Resp / Err.
func (m *Mock) Complete(_ context.Context, req Request) (*Response, error) {
	m.Calls = append(m.Calls, req)
	if m.Err != nil {
		return nil, m.Err
	}
	if m.Resp != nil {
		return m.Resp, nil
	}
	return &Response{Provider: m.Provider(), Model: m.Model()}, nil
}
