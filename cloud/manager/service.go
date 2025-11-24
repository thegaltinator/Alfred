package manager

import (
	"context"
	"errors"
)

// Service is the Manager that delegates to the GPT-5 Mini model.
type Service struct {
	model *LLMClient
}

// NewServiceFromEnv constructs the Manager service from environment variables.
func NewServiceFromEnv() (*Service, error) {
	client, err := NewLLMClientFromEnv()
	if err != nil {
		return nil, err
	}
	return &Service{model: client}, nil
}

// Decide implements the decisioner interface by invoking the model.
func (s *Service) Decide(ctx context.Context, evt Event) (Decision, error) {
	if s == nil || s.model == nil {
		return Decision{}, errors.New("manager service not initialized")
	}
	return s.model.Decide(ctx, evt)
}
