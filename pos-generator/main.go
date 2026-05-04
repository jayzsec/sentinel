package main

import (
	"encoding/json"
	"fmt"
	"math/rand/v2"
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

	fmt.Println("Starting POS Event Stream...")
	fmt.Println("Press Ctrl+C to stop.")

	// 2. Generate a single event to test our logic
	// testEvent := generateNormalEvent(rng)

	// Create a ticker that fires every 2 seconds to simulate busy traffic
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop() // Good practice to clean up resources

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

		// Use json.Marshal instead of MarshalIndent so each event is on a single line.
		// This is standard practice for logging streams and piping to other applications.
		jsonData, err := json.Marshal(currentEvent)
		if err != nil {
			fmt.Printf(`{"error": "Failed to generate JSON: %s"}\n`, err)
			continue
		}

		// Print the JSON string to standard output
		fmt.Println(string(jsonData))
	}

	// 3. Convert our Go struct into a beautifully formatted JSON string (Ticket 2.3)
	// jsonData, err := json.MarshalIndent(testEvent, "", " ")
	// if err != nil {
	// 	fmt.Println("Error generating JSON:", err)
	// 	return
	// }

	// 4. Print it to the console
	// fmt.Println("Simulated POS Event:")
	// fmt.Println(string(jsonData))
}
