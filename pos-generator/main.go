package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
)

// POSEvent represents a single action taken on a POS terminal
type POSEvent struct {
	EventID       string  `json:"event_id"`
	VenueID       string  `json:"venue_id"`
	Terminal      string  `json:"terminal"`
	StaffRole     string  `json:"staff_role"`
	Action        string  `json:"action"`
	Amount        float64 `json:"amount"`
	ItemID        string  `json:"item_id,omitempty"`
	StaffID       string  `json:"staff_id"`
	ManagerID     string  `json:"manager_id,omitempty"`
	PaymentMethod string  `json:"payment_method,omitempty"`
	Timestamp     string  `json:"timestamp"`
}

// Global dictionaries
var terminals = []string{"Front Bar 1", "Front Bar 2", "Dining Room POS", "Patio Mobile"}
var staffRoles = []string{"Manager", "Bartender", "Server", "Temporary Seasonal Staff"}
var normalActions = []string{"Order", "Payment"}
var venues = []string{"V-Brisbane-CBD", "V-GoldCoast", "V-SunshineCoast"}

// generateNormalEvent creates a perfectly healthy, standard restaurant transaction.
func generateNormalEvent(rng *rand.Rand, venueID string) POSEvent {
	randomTerminal := terminals[rng.IntN(len(terminals))]
	randomRole := staffRoles[rng.IntN(len(staffRoles))]
	randomAction := normalActions[rng.IntN(len(normalActions))]
	randomAmount := 5.0 + (rng.Float64() * 150.0)

	return POSEvent{
		EventID:   uuid.NewString(),
		VenueID:   venueID,
		Terminal:  randomTerminal,
		StaffRole: randomRole,
		Action:    randomAction,
		Amount:    randomAmount,
		Timestamp: time.Now().Format(time.RFC3339),
	}
}

// simulateSweethearting returns a chronological array of events representing theft.
func simulateSweetHearting(venueID string) []POSEvent {
	var timeline []POSEvent

	terminal := "main-bar-register"
	staffID := "s-12"
	managerID := "m-402"
	itemID := "item-ribeye-01"

	// We simulate this table starting their meal exactly one hour ago
	baseTime := time.Now().Add(-1 * time.Hour)

	// T+0: Initial Order
	timeline = append(timeline, POSEvent{
		EventID:   uuid.NewString(),
		VenueID:   venueID,
		Timestamp: baseTime.Format(time.RFC3339),
		Terminal:  terminal,
		Action:    "Order",
		Amount:    65.00,
		ItemID:    itemID,
		StaffID:   staffID,
	})

	// T+45 mins: Ordering drinks (Adding noise to make the timeline look like a real table)
	timeline = append(timeline, POSEvent{
		EventID:   uuid.NewString(),
		VenueID:   venueID,
		Timestamp: baseTime.Add(45 * time.Minute).Format(time.RFC3339),
		Terminal:  terminal,
		Action:    "Order",
		Amount:    20.00,
		ItemID:    "ITEM-BEER-02",
		StaffID:   staffID,
	})

	// T+46 mins: The Malicious Void by the Manager (The theft)
	timeline = append(timeline, POSEvent{
		EventID:   uuid.NewString(),
		VenueID:   venueID,
		Timestamp: baseTime.Add(46 * time.Minute).Format(time.RFC3339),
		Terminal:  terminal,
		Action:    "Void",
		Amount:    65.00,
		ItemID:    itemID,
		StaffID:   staffID,
		ManagerID: managerID, // Manager PIN used to authorize the void
	})

	// T+47 mins: The Settlement (The getaway)
	timeline = append(timeline, POSEvent{
		EventID:       uuid.NewString(),
		VenueID:       venueID,
		Timestamp:     baseTime.Add(47 * time.Minute).Format(time.RFC3339),
		Terminal:      terminal,
		Action:        "Payment",
		Amount:        20.00, // Only paying for the beers
		StaffID:       staffID,
		PaymentMethod: "Cash", // Cash is untraceable, completing the sweethearting profile
	})

	return timeline
}

// transmitEvent handles the JSON marshaling and HTTP POST to the Sentinel
func transmitEvent(client *http.Client, targetURL string, event POSEvent) {
	jsonData, err := json.Marshal(event)
	if err != nil {
		fmt.Printf("[ERROR] Failed to marshal JSON: %v\n", err)
		return
	}

	req, _ := http.NewRequest(http.MethodPost, targetURL, bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[ERROR] Network failure reaching Sentinel: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if event.Action == "Void" {
		fmt.Printf("-> Broadcasted Anomaly (Void) to network for Venue: %s\n", event.VenueID)
	} else {
		fmt.Print(".")
	}
}

func main() {
	// Initialise randomiser
	seed := uint64(time.Now().UnixNano())
	rng := rand.New(rand.NewPCG(seed, seed))

	// We check the environment for a URL. If it's empty, we fall back to localhost.
	sentinelURL := os.Getenv("SENTINEL_URL")
	if sentinelURL == "" {
		sentinelURL = "http://localhost:8080/events"
	}

	// Add the new environment variable for our C# API
	analyticsURL := os.Getenv("ANALYTICS_URL")
	if analyticsURL == "" {
		analyticsURL = "http://localhost:5192/ingest"
	}

	fmt.Println("POS Event Stream Generator Online.")
	fmt.Printf("Transmitting live data to Sentinel at: %s\n", sentinelURL)
	fmt.Println("Press Ctrl+C to stop.")

	// Create a ticker that fires every 5 seconds to simulate busy traffic
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop() // Good practice to clean up resources

	// We create a reusable HTTP client. It's a best practice to set a timeout
	// so our generator doesn't freeze if the Sentinel crashes.
	client := &http.Client{
		Timeout: 5 * time.Second,
	}

	// Start an infinite loop to listen to the ticker
	for {
		<-ticker.C // Block until the ticker fires

		currentVenue := venues[rng.IntN(len(venues))]
		isMalicious := rng.Float32() < 0.05 // Fixed: < 0.05 correctly yields a 5% chance

		if isMalicious {
			// Generate the Sweetheart Timeline
			fmt.Printf("[!] Initiating Sweethearting Simulation at %s...\n", currentVenue)
			maliciousTimeline := simulateSweetHearting(currentVenue)

			// Safely transmit the entire array sequentially inside the loop
			for _, event := range maliciousTimeline {
				transmitEvent(client, sentinelURL, event)
				time.Sleep(500 * time.Millisecond) // Ensure chronological ingestion
			}
		} else {
			// Generate and immediately transmit a normal transaction
			normalEvent := generateNormalEvent(rng, currentVenue)
			transmitEvent(client, sentinelURL, normalEvent)
		}
		// Send the HTTP POST request to the Sentinel
		// Update - Fire to Sentinel in a concurrent Goroutine
		//go func() {
		//	req, _ := http.NewRequest(http.MethodPost, sentinelURL, bytes.NewBuffer(jsonData))
		//	req.Header.Set("Content-Type", "application/json")
		//	resp, err := client.Do(req)
		//	if err == nil {
		//		resp.Body.Close()
		//	}
		//}()
		//
		//// Fire to Analytics Engine in a concurrent Goroutine
		//go func() {
		//	req, _ := http.NewRequest(http.MethodPost, analyticsURL, bytes.NewBuffer(jsonData))
		//	req.Header.Set("Content-Type", "application/json")
		//	resp, err := client.Do(req)
		//	if err == nil {
		//		resp.Body.Close()
		//	}
		//}()
	}
}
