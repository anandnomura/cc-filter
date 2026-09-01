package proposals

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"

	"bap-system/internal/authzen"
)

type Proposal struct {
	ID           string    `json:"id"`
	SubjectType  string    `json:"subject_type"`
	Action       string    `json:"action"`
	Tool         string    `json:"tool"`
	ResourceType string    `json:"resource_type"`
	FirstSeen    time.Time `json:"first_seen"`
}

type Summary struct {
	Proposal
	Occurrences int       `json:"occurrences"`
	LastSeen    time.Time `json:"last_seen"`
}

type Store struct {
	path string
	mu   sync.Mutex
}

func New(path string) *Store { return &Store{path: path} }

// Record stores only non-sensitive classification data, never paths or commands.
func (s *Store) Record(request authzen.EvaluationRequest) (string, error) {
	tool, _ := request.Resource.Properties["tool"].(string)
	id := proposalID(request.Subject.Type, request.Action.Name, tool, request.Resource.Type)
	proposal := Proposal{
		ID: id, SubjectType: request.Subject.Type, Action: request.Action.Name,
		Tool: tool, ResourceType: request.Resource.Type, FirstSeen: time.Now().UTC(),
	}
	data, err := json.Marshal(proposal)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	file, err := os.OpenFile(s.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.Write(append(data, '\n')); err != nil {
		return "", err
	}
	return id, nil
}

func Summarize(path string) ([]Summary, error) {
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return []Summary{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()
	byID := map[string]Summary{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var proposal Proposal
		if err := json.Unmarshal(scanner.Bytes(), &proposal); err != nil {
			return nil, fmt.Errorf("parse proposal log: %w", err)
		}
		summary := byID[proposal.ID]
		if summary.Occurrences == 0 {
			summary.Proposal = proposal
			summary.LastSeen = proposal.FirstSeen
		}
		summary.Occurrences++
		if proposal.FirstSeen.After(summary.LastSeen) {
			summary.LastSeen = proposal.FirstSeen
		}
		byID[proposal.ID] = summary
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	result := make([]Summary, 0, len(byID))
	for _, summary := range byID {
		result = append(result, summary)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Occurrences > result[j].Occurrences })
	return result, nil
}

func proposalID(parts ...string) string {
	hash := sha256.New()
	for _, part := range parts {
		_, _ = hash.Write([]byte(part))
		_, _ = hash.Write([]byte{0})
	}
	return "proposal-" + hex.EncodeToString(hash.Sum(nil))[:16]
}
