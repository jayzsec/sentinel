package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"
)

// POSEvent represents a single action taken on a POS terminal
type POSEvent struct {
	EventID   string    `json:"event_id"`
	Terminal  string    `json:"terminal"`
	StaffRole string    `json:"staff_role"`
	Action    string    `json:"action"`
	Amount    float64   `json:"amount"`
	Timestamp time.Time `json:"timestamp"`
}

// --- TICKET 2.1: The Dictionaries ---
// We define these as global slices so our generator can pick from them quickly.
var terminals = []string{"Front Bar 1", "Front Bar 2", "Dining Room POS", "Patio Mobile"}
var staffRoles = []string{"Manager", "Bartender", "Server", "Temporary Seasonal Staff"}
var normalActions = []string{"Order", "Payment"}

// --- TICKET 2.2: The Generator Function ---
// generateNormalEvent creates a perfectly healthy, standard restaurant transaction.
func generateNormalEvent(rng *rand.Rand) POSEvent {
	// Pick random indexes from our dictionaries
	randomTerminal := terminals[rng.IntN(len(terminals))]
	randomRole := staffRoles[rng.IntN(len(staffRoles))]
	randomAction := normalActions[rng.IntN(len(normalActions))]

	// Generate a random amount between $5.00 and $155.00 for normal orders
	randomAmount := 5.0 + (rng.Float64() * 150.0)

	return POSEvent{
		EventID:   fmt.Sprintf("EVT-%d", rng.IntN(1000000)), // Fake random ID
		Terminal:  randomTerminal,
		StaffRole: randomRole,
		Action:    randomAction,
		Amount:    randomAmount,
		Timestamp: time.Now(),
	}
}

// --- TICKET 3.1: The Threat Injector ---
// generateAnomalousEvent creates a highly suspicious transaction for our AI to catch.
func generateAnomalousEvent(rng *rand.Rand) POSEvent {
	// Anomalies often happen at less supervised terminals
	suspiciousTerminal := "Patio Mobile"

	// We simulate a compromised account or insider threat
	vulnerableRole := "Temporary Seasonal Staff"

	// The malicious action
	suspiciousAction := "Void"

	// High dollar amount to trigger the agent's threshold quickly ($200 to $500)
	suspiciousAmount := 200.0 + (rng.Float64() * 300.0)

	return POSEvent{
		EventID:   fmt.Sprintf("EVT-CRIT-%d", rng.IntN(1000000)), // Tagging ID for easier debugging
		Terminal:  suspiciousTerminal,
		StaffRole: vulnerableRole,
		Action:    suspiciousAction,
		Amount:    suspiciousAmount,
		Timestamp: time.Now(),
	}
}

func main() {
	// 1. Initialize our random number generator with the current time
	// This ensures we get different results every time we run the program.
	seed := uint64(time.Now().UnixNano())
	rng := rand.New(rand.NewPCG(seed, seed))

	// The URL where our Sentinel is listening
	sentinelURL := "http://localhost:8080/events"

	fmt.Println("POS Event Stream Generator Online.")
	fmt.Printf("Transmitting live data to Sentinel at: %s\n", sentinelURL)
	fmt.Println("Press Ctrl+C to stop.")

	// Create a ticker that fires every 2 seconds to simulate busy traffic
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop() // Good practice to clean up resources

	// We create a reusable HTTP client. It's a best practice to set a timeout
	// so our generator doesn't freeze if the Sentinel crashes.
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Start an infinite loop to listen to the ticker
	for {
		<-ticker.C // Block until the ticker fires
		var currentEvent POSEvent

		// 15% chance to generate a suspicious anomaly
		if rng.Float64() < 0.15 {
			currentEvent = generateAnomalousEvent(rng)
		} else {
			currentEvent = generateNormalEvent(rng)
		}

		// Convert our struct to JSON
		jsonData, err := json.Marshal(currentEvent)
		if err != nil {
			fmt.Printf("Error generating JSON: %s\n", err)
			continue
		}

		// Send the HTTP POST request to the Sentinel
		req, err := http.NewRequest(http.MethodPost, sentinelURL, bytes.NewBuffer(jsonData))
		if err != nil {
			fmt.Printf("Error creating request: %s\n", err)
			continue
		}
		// Tell the Sentinel we are sending JSON data
		req.Header.Set("Content-Type", "application/json")

		// Execute the request
		resp, err := client.Do(req)
		if err != nil {
			// If the Sentinel is down, we catch the error but keep the generator running!
			fmt.Printf("\n[!] Connection Error: Sentinel is unreachable (%s)\n", err.Error())
			continue
		}

		// Close the response body to prevent memory leaks (Crucial Go best practice!)
		if err := resp.Body.Close(); err != nil {
			fmt.Printf("Error closing response body: %s\n", err)
		}

		// Print local feedback
		if currentEvent.Action == "Void" {
			fmt.Printf("-> Sent Anomaly (Void) to Sentinel. Status: %d\n", resp.StatusCode)
		} else {
			fmt.Printf(".")
		}
	}
}
