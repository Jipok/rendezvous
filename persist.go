package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
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

	// Get the directory of the persistence file
	dir := filepath.Dir(persistenceFile)

	// Create a temporary file in the same directory (to ensure atomicity on rename)
	tempFile, err := os.CreateTemp(dir, "store_temp_*.json")
	if err != nil {
		log.Printf("Error creating temporary file: %v", err)
		return
	}

	// Write data to the temporary file
	if _, err := tempFile.Write(data); err != nil {
		log.Printf("Error writing data to temporary file: %v", err)
		tempFile.Close()
		os.Remove(tempFile.Name())
		return
	}

	// Close the temporary file
	if err := tempFile.Close(); err != nil {
		log.Printf("Error closing temporary file: %v", err)
		os.Remove(tempFile.Name())
		return
	}

	// Rename temporary file to the target persistence file (atomic operation)
	if err := os.Rename(tempFile.Name(), persistenceFile); err != nil {
		log.Printf("Error renaming temporary file to %s: %v", persistenceFile, err)
		os.Remove(tempFile.Name())
		return
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
	}
}
