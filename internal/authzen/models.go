package authzen

import (
	"errors"
	"fmt"
)

// Entity is the AuthZEN subject/resource information model.
type Entity struct {
	Type       string         `json:"type"`
	ID         string         `json:"id"`
	Properties map[string]any `json:"properties,omitempty"`
}

// Action is the AuthZEN action information model.
type Action struct {
	Name       string         `json:"name"`
	Properties map[string]any `json:"properties,omitempty"`
}

// EvaluationRequest follows OpenID AuthZEN Authorization API 1.0.
type EvaluationRequest struct {
	Subject  Entity         `json:"subject"`
	Action   Action         `json:"action"`
	Resource Entity         `json:"resource"`
	Context  map[string]any `json:"context,omitempty"`
}

func (r EvaluationRequest) Validate() error {
	if r.Subject.Type == "" || r.Subject.ID == "" {
		return errors.New("subject.type and subject.id are required")
	}
	if r.Action.Name == "" {
		return errors.New("action.name is required")
	}
	if r.Resource.Type == "" || r.Resource.ID == "" {
		return errors.New("resource.type and resource.id are required")
	}
	return nil
}

func StringProperty(properties map[string]any, name string) (string, error) {
	value, ok := properties[name].(string)
	if !ok {
		return "", fmt.Errorf("resource.properties.%s must be a string", name)
	}
	return value, nil
}
