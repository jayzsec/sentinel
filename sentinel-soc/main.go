package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// We need the exact same data contract here to understand the incoming data.
type POSEvent struct {
	EventID   string    `json:"event_id"`
	Terminal  string    `json:"terminal"`
	StaffRole string    `json:"staff_role"`
	Action    string    `json:"action"`
	Amount    float64   `json:"amount"`
	Timestamp time.Time `json:"timestamp"`
}

// This map tracks the total dollar amount of voids per terminal.
// Moved to global scope for our HTTP handler
var voidTracker = make(map[string]float64)

// AIResponse represents the JSON payload we expect back from our LLM.
type AIResponse struct {
	Action string `json:"action"` // "alert_manager" or "lockdown"
	Reason string `json:"reason"`
}

// This function handles incoming HTTP POST requests
func handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	var event POSEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	// Process the event exactly as we did before
	if event.Action == "Void" {
		voidTracker[event.Terminal] += event.Amount
		fmt.Printf("[-] Suspicious Action Tracked: %s logged a $%.2f void on %s.\n", event.StaffRole, event.Amount, event.Terminal)
		if voidTracker[event.Terminal] >= 200.0 {
			aiDecision := evaluateThreatWithAI(event, voidTracker[event.Terminal])
			executeMitigation(aiDecision, event.Terminal)
			voidTracker[event.Terminal] = 0.0
		}
	} else {
		fmt.Print(".")
	}

	// Forward ALL events to the C# Analytics Engine asynchronously
	// Using a goroutine prevents network latency from slowing down our Go Sentinel
	go forwardToAnalytics(event)

	// Tell the generator we received it successfully
	w.WriteHeader(http.StatusOK)
}

// --- TICKET 4.4: The Agentic Reasoning ---
// This function packages the context and "sends" it to the AI.
func evaluateThreatWithAI(event POSEvent, totalVoided float64) AIResponse {
	fmt.Println("\n[!] THRESHOLD BREACHED. Contacting AI Agent...")

	// This is the exact prompt we would send to an LLM API (like OpenAI or Ollama)
	systemPrompt := fmt.Sprintf(`
		You are an autonomous SOC agent. 
		Context: Terminal '%s' operated by '%s' just processed a '%s'.
		Total suspicious activity value on this terminal recently: $%.2f.
		If this indicates high probability of internal shrinkage (theft), output JSON {"action": "lockdown", "reason": "<your reasoning>"}.
		Otherwise, output {"action": "alert_manager", "reason": "<your reasoning>"}.`,
		event.Terminal, event.StaffRole, event.Action, totalVoided)

	fmt.Printf(">> SYSTEM PROMPT SENT TO LLM:\n%s\n\n", systemPrompt)

	// To make this runnable today, we simulate the LLM's response.
	// If it's temporary staff doing huge voids, the AI decides to lock it down.
	time.Sleep(1 * time.Second) // Simulate network latency to the AI

	if event.StaffRole == "Temporary Seasonal Staff" && totalVoided > 200.0 {
		return AIResponse{
			Action: "lockdown",
			Reason: "High-value void detected by temporary seasonal staff exceeding safety thresholds. High probability of internal shrinkage.",
		}
	}

	return AIResponse{
		Action: "alert_manager",
		Reason: "Void activity is elevated but fits normal parameters for this staff role.",
	}
}

// --- TICKET 4.3 & 4.4: The automated response ---
func executeMitigation(response AIResponse, terminal string) {
	if response.Action == "lockdown" {
		fmt.Printf("[CRITICAL ACTION] Executing network isolation for container running %s.\n", terminal)
		fmt.Printf("[REASON] %s\n", response.Reason)
		// Here is where you would use Go's os/exec package to run Terraform or Incus commands!
	} else {
		fmt.Printf("[WARNING] Paging floor manager to review %s.\n", terminal)
	}
	fmt.Println("--------------------------------------------------")
}

// forwardToAnalytics pushes the evaluated event downstream to the C# API
func forwardToAnalytics(event POSEvent) {
	analyticsURL := os.Getenv("ANALYTICS_URL")
	if analyticsURL == "" {
		fmt.Printf("[ERROR] ANALYTICS_URL is not set. Data dropped.")
		return
	}

	// 1. Serialize the event back into JSON
	payloadBytes, err := json.Marshal(event)
	if err != nil {
		fmt.Printf("[ERROR] Failed to marshal event for forwarding: %v\n", err)
		return
	}

	// 2. Execute the HTTP POST request to the C# container
	req, err := http.NewRequest("POST", analyticsURL, bytes.NewBuffer(payloadBytes))
	if err != nil {
		fmt.Printf("[ERROR] Failed to create HTTP request: %v\n", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		fmt.Printf("[ERROR] Network failure forwarding to Analytics Engine: %v\n", err)
	}

	defer resp.Body.Close()

	// 3. Verify the C# API accepted it
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		fmt.Printf("[->] Successfully forwarded event to Analytics Engine (Status: %d)\n", resp.StatusCode)
	} else {
		fmt.Printf("[!] Analytics Engine rejected payload. Status: %d\n", resp.StatusCode)
	}

}

func main() {
	fmt.Println("Sentinel SOC Engine Online.")
	fmt.Println("Listening for HTTP POST requests on port 8080...")

	// Route traffic coming to /events to our handler function
	http.HandleFunc("/events", handleEvent)

	// Start the server
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Server failed to start:", err)
	}
}
