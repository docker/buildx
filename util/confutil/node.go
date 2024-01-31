package confutil

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"sync"
)

var nodeIdentifierMu sync.Mutex

func TryNodeIdentifier(configDir string) (out string) {
	nodeIdentifierMu.Lock()
	defer nodeIdentifierMu.Unlock()
	sessionFile := filepath.Join(configDir, ".buildNodeID")
	if _, err := os.Lstat(sessionFile); err != nil {
		if os.IsNotExist(err) { // create a new file with stored randomness
			b := make([]byte, 8)
			if _, err := rand.Read(b); err != nil {
				return out
			}
			if err := os.WriteFile(sessionFile, []byte(hex.EncodeToString(b)), 0600); err != nil {
				return out
			}
		}
	}

	dt, err := os.ReadFile(sessionFile)
	if err == nil {
		return string(dt)
	}
	return
}
