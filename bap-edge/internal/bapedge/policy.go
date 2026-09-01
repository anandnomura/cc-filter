package bapedge

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"bap-system/internal/grants"
	"bap-system/internal/policybundle"
)

const EdgeProtocolVersion = "3"

type policyState struct {
	HighestVersion  uint64    `json:"highest_version"`
	RulesDigest     string    `json:"rules_digest"`
	RevocationEpoch uint64    `json:"revocation_epoch"`
	LastSync        time.Time `json:"last_sync"`
}

type PolicyStore struct {
	directory string
	publicKey ed25519.PublicKey
}

func NewPolicyStore(config Config) (*PolicyStore, error) {
	base, err := stateDirectory(config.StateDirectory)
	if err != nil {
		return nil, err
	}
	directory := filepath.Join(base, "policy")
	if err := os.MkdirAll(directory, 0700); err != nil {
		return nil, err
	}
	publicKey, err := grants.LoadPublicKey(config.BundlePublicKeyPath)
	if err != nil {
		return nil, fmt.Errorf("load BAP bundle verification key: %w", err)
	}
	return &PolicyStore{directory: directory, publicKey: publicKey}, nil
}

func LoadOrCreateEdgeInstanceID(configured string) (string, error) {
	base, err := stateDirectory(configured)
	if err != nil {
		return "", err
	}
	path := filepath.Join(base, "edge-instance.json")
	if data, readErr := os.ReadFile(path); readErr == nil {
		var value struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(data, &value) == nil && value.ID != "" {
			return value.ID, nil
		}
		return "", errors.New("invalid Edge instance identity")
	} else if !os.IsNotExist(readErr) {
		return "", readErr
	}
	value := struct {
		ID string `json:"id"`
	}{ID: "bape_" + randomHex(24)}
	data, _ := json.Marshal(value)
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", err
	}
	return value.ID, nil
}

func EnsurePolicy(ctx context.Context, client *Client, store *PolicyStore, edgeInstanceID string, force bool, now time.Time) (policybundle.Bundle, error) {
	if !force && !store.NeedsRefresh(now) {
		bundle, _, err := store.Current(now)
		return bundle, err
	}
	version, digest, epoch := store.Posture()
	response, err := client.SyncPolicy(ctx, policybundle.SyncRequest{
		EdgeInstanceID: edgeInstanceID, EdgeVersion: EdgeProtocolVersion, InstalledVersion: version,
		InstalledDigest: digest, RevocationEpoch: epoch, Nonce: randomHex(16),
	})
	if err != nil {
		bundle, _, localErr := store.Current(now)
		if localErr == nil {
			return bundle, nil
		}
		return policybundle.Bundle{}, err
	}
	bundle, err := store.Accept(response.Envelope, now)
	if err != nil {
		return bundle, err
	}
	if err := checkProtocolVersion(EdgeProtocolVersion, bundle.MinimumEdgeVersion); err != nil {
		return bundle, fmt.Errorf("BAP Edge protocol %s is below required version %s", EdgeProtocolVersion, bundle.MinimumEdgeVersion)
	}
	if response.Directive == "KILL_SWITCH" || bundle.KillSwitch {
		return bundle, errors.New("BAP policy kill switch is active")
	}
	return bundle, nil
}

func checkProtocolVersion(current, required string) error {
	requiredVersion, requiredErr := strconv.ParseUint(required, 10, 64)
	currentVersion, currentErr := strconv.ParseUint(current, 10, 64)
	if requiredErr != nil || currentErr != nil {
		return errors.New("invalid numeric BAP Edge protocol version")
	}
	if requiredVersion > currentVersion {
		return errors.New("BAP Edge protocol version is too old")
	}
	return nil
}

func (s *PolicyStore) Current(now time.Time) (policybundle.Bundle, policyState, error) {
	state, err := s.loadState()
	if err != nil {
		return policybundle.Bundle{}, state, err
	}
	data, err := os.ReadFile(filepath.Join(s.directory, "active-bundle.json"))
	if err != nil {
		return policybundle.Bundle{}, state, err
	}
	var envelope policybundle.Envelope
	if err := json.Unmarshal(data, &envelope); err != nil {
		return policybundle.Bundle{}, state, errors.New("invalid stored policy envelope")
	}
	bundle, err := policybundle.Verify(s.publicKey, envelope, now)
	if err != nil {
		return bundle, state, err
	}
	if bundle.Version != state.HighestVersion || bundle.RulesDigest != state.RulesDigest || bundle.RevocationEpoch < state.RevocationEpoch {
		return bundle, state, errors.New("stored policy bundle violates rollback state")
	}
	if state.LastSync.IsZero() || now.UTC().Sub(state.LastSync) > time.Duration(bundle.MaxOfflineSeconds)*time.Second {
		return bundle, state, errors.New("policy synchronization lease expired")
	}
	return bundle, state, nil
}

func (s *PolicyStore) NeedsRefresh(now time.Time) bool {
	bundle, state, err := s.Current(now)
	if err != nil {
		return true
	}
	return now.UTC().Sub(state.LastSync) >= time.Duration(bundle.RefreshAfterSeconds)*time.Second
}

func (s *PolicyStore) Posture() (uint64, string, uint64) {
	state, err := s.loadState()
	if err != nil {
		return 0, "", 0
	}
	return state.HighestVersion, state.RulesDigest, state.RevocationEpoch
}

func (s *PolicyStore) Accept(envelope policybundle.Envelope, now time.Time) (policybundle.Bundle, error) {
	bundle, err := policybundle.Verify(s.publicKey, envelope, now)
	if err != nil {
		return bundle, err
	}
	state, stateErr := s.loadState()
	if stateErr != nil && !os.IsNotExist(stateErr) {
		return bundle, stateErr
	}
	if bundle.Version < state.HighestVersion || bundle.RevocationEpoch < state.RevocationEpoch {
		return bundle, errors.New("policy bundle rollback rejected")
	}
	if bundle.Version == state.HighestVersion && state.RulesDigest != "" && bundle.RulesDigest != state.RulesDigest {
		return bundle, errors.New("policy bundle version equivocation rejected")
	}
	data, _ := json.Marshal(envelope)
	if err := atomicWrite(filepath.Join(s.directory, "active-bundle.json"), data); err != nil {
		return bundle, err
	}
	state = policyState{HighestVersion: bundle.Version, RulesDigest: bundle.RulesDigest, RevocationEpoch: bundle.RevocationEpoch, LastSync: now.UTC()}
	stateData, _ := json.Marshal(state)
	if err := atomicWrite(filepath.Join(s.directory, "policy-state.json"), stateData); err != nil {
		return bundle, err
	}
	return bundle, nil
}

func (s *PolicyStore) loadState() (policyState, error) {
	var state policyState
	data, err := os.ReadFile(filepath.Join(s.directory, "policy-state.json"))
	if err != nil {
		return state, err
	}
	if err := json.Unmarshal(data, &state); err != nil || state.HighestVersion == 0 || state.RulesDigest == "" {
		return state, errors.New("invalid policy rollback state")
	}
	return state, nil
}

func atomicWrite(path string, data []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, data, 0600); err != nil {
		return err
	}
	backup := path + ".bak"
	hadExisting := false
	if _, err := os.Stat(path); err == nil {
		_ = os.Remove(backup)
		if err := os.Rename(path, backup); err != nil {
			_ = os.Remove(temporary)
			return err
		}
		hadExisting = true
	} else if !os.IsNotExist(err) {
		_ = os.Remove(temporary)
		return err
	}
	if err := os.Rename(temporary, path); err != nil {
		if hadExisting {
			_ = os.Rename(backup, path)
		}
		_ = os.Remove(temporary)
		return err
	}
	_ = os.Remove(backup)
	return nil
}
