package inbox

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// reserveRequest durably counts an attempt before contacting a paid provider.
// Failures and interrupted requests retain their reservation. The exclusive lock
// also prevents two application processes from spending the same final slot.
// A crash may leave a lock behind; this fails closed until an operator checks it.
func reserveRequest(path string, limit int) error {
	if path == "" || limit < 1 {
		return errors.New("AI request limit is not configured")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return errors.New("cannot create AI request ledger directory")
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("AI request ledger is locked or unavailable; inspect it before retrying")
	}
	_ = lock.Close()
	defer os.Remove(path + ".lock")
	var ledger struct {
		Version  int `json:"version"`
		Reserved int `json:"reserved_requests"`
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		ledger.Version = 1
	} else if err != nil || json.Unmarshal(data, &ledger) != nil || ledger.Version != 1 || ledger.Reserved < 0 {
		return errors.New("AI request ledger is unreadable or invalid; requests are blocked")
	}
	if ledger.Reserved >= limit {
		return errors.New("AI request limit reached; no provider request was sent")
	}
	ledger.Reserved++
	temp, err := os.CreateTemp(filepath.Dir(path), ".ai-ledger-*")
	if err != nil {
		return errors.New("cannot reserve an AI request")
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	data = append(jsonBytes(ledger), '\n')
	_, writeErr := temp.Write(data)
	syncErr := temp.Sync()
	closeErr := temp.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errors.New("could not persist AI request reservation")
	}
	if err := os.Rename(tempName, path); err != nil {
		return errors.New("could not commit AI request reservation")
	}
	return nil
}
