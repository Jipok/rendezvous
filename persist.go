package main

import (
	"encoding/json"
	"log"
	"os"
	"time"
)

var persistenceFile = "store.json"

// saveKVStore saves the current key-value store to a JSON file.
func saveKVStore() {
	mu.RLock()
	// Create a temporary map for persistence that holds key -> value
	persistMap := make(map[string][]byte, len(kvStore))
	for key, entry := range kvStore {
		persistMap[key] = entry.Value
	}
	mu.RUnlock()

	data, err := json.Marshal(persistMap)
	if err != nil {
		log.Printf("Error marshaling kvStore: %v", err)
		return
	}
	if err := os.WriteFile(persistenceFile, data, 0644); err != nil {
		log.Printf("Error writing kvStore to file: %v", err)
	}
}

// loadKVStore loads the key-value store from a JSON file.
// It sets LastUpdate to the current time for all loaded keys.
func loadKVStore() {
	data, err := os.ReadFile(persistenceFile)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("Error reading persistence file: %v", err)
		}
		return
	}

	var persistMap map[string][]byte
	if err := json.Unmarshal(data, &persistMap); err != nil {
		log.Printf("Error unmarshaling kvStore: %v", err)
		return
	}

	now := time.Now()
	mu.Lock()
	for key, value := range persistMap {
		kvStore[key] = &Entry{
			Value:      value,
			LastUpdate: now,
		}
	}
	mu.Unlock()
	log.Printf("Loaded %d keys from persistence file", len(persistMap))
}

// periodicSave saves the store every 30 minutes
func periodicSave() {
	ticker := time.NewTicker(*saveDuration)
	defer ticker.Stop()
	for {
		<-ticker.C
		saveKVStore()
		log.Println("Periodic saving done")
	}
}
