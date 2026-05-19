package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
	"go.opentelemetry.io/otel/trace"
)

// POSEvent We need the exact same data contract here to understand the incoming data.
type POSEvent struct {
	EventID       string  `json:"event_id"`
	VenueID       string  `json:"venue_id"`
	Timestamp     string  `json:"timestamp"`
	Terminal      string  `json:"terminal"`
	Action        string  `json:"action"`
	Amount        float64 `json:"amount"`
	ItemID        string  `json:"item_id,omitempty"`
	StaffID       string  `json:"staff_id"`
	ManagerID     string  `json:"manager_id,omitempty"`
	PaymentMethod string  `json:"payment_method,omitempty"`
}

// Global Redis Client
var rdb *redis.Client

// AIResponse represents the JSON payload we expect back from our LLM.
type AIResponse struct {
	Action string `json:"action"` // "alert_manager" or "lockdown"
	Reason string `json:"reason"`
}

// initTracer wires up OpenTelemetry to export traces via HTTP OTLP
func initTracer() *sdktrace.TracerProvider {
	ctx := context.Background()
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	// 1. Configure the OTLP Exporter
	// This takes the traces and fires them over the network instead of printing them to the console.
	// We allow the endpoint to be injected via environment variables for cloud deployment.

	// Fix: Appsotlptracehttp to otlptracegrpc
	// Port 4317 is the industry standard port for OpenTelemetry over gRPC, previous we are using Port 4318 which is the standard port for OpenTelemetry over HTTP
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithInsecure(), // Used for internal cluster routing
		otlptracegrpc.WithEndpoint(endpoint),
	)
	if err != nil {
		fmt.Printf("[FATAL] Failed to initialize OTLP exporter: %v\n", err)
		os.Exit(1)
	}

	// 2. Define the Resource Attributes
	// This tells the observability dashboard exactly WHICH microservice generated the trace.
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("sentinel-soc"),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		fmt.Printf("[FATAL] Failed to create resource: %v\n", err)
		os.Exit(1)
	}

	// 3. Register the Provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{}) // Crucial for passing trace IDs to C#
	return tp
}

func initRedis() {
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "localhost:6379"
	}

	redisPassword := os.Getenv("REDIS_PASSWORD")

	// Configure Redis
	options := &redis.Options{
		Addr:     redisAddr,
		Password: redisPassword,
		DB:       0,
	}

	// AZURE FIX: If connecting to Azure Redis (port 6380), we MUST enable TLS
	if strings.HasSuffix(redisAddr, "6380") {
		options.TLSConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
		}
	}

	rdb = redis.NewClient(options)

	// Test connection
	if err := rdb.Ping(context.Background()).Err(); err != nil {
		fmt.Printf("[FATAL] Could not connect to Redis at %s: %v\n", redisAddr, err)
		os.Exit(1)
	}

	fmt.Printf("[+] Connected to Distributed State Store (Redis) at %s\n", redisAddr)
}

// This function handles incoming HTTP POST requests
func handleEvent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. START OBSERVABILITY TRACE
	// We extract the context from the incoming HTTP request
	ctx := r.Context()
	tracer := otel.Tracer("sentinel-soc")
	ctx, span := tracer.Start(ctx, "IngestPOSTEvent")
	defer span.End() // Ensures the span closes and calculates total duration when function exits

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

	// Tag the span with searchable metadata
	span.SetAttributes(
		attribute.String("venue.id", event.VenueID),
		attribute.String("terminal.id", event.Terminal),
		attribute.String("event.action", event.Action),
	)

	//ctx := context.Background()

	// 2. DISTRIBUTED STATE
	// Create a unique Redis key for this specific terminal at this specific venue
	redisKey := fmt.Sprintf("timeline:%s:%s", event.VenueID, event.Terminal)
	eventJSON, _ := json.Marshal(event)

	// Pipeline the commands to save network round trips
	pipe := rdb.Pipeline()
	// Note we use the traced 'ctx' here
	pipe.LPush(ctx, redisKey, eventJSON)
	pipe.LTrim(ctx, redisKey, 0, 49) // Keep exactly 50 events for the sliding window
	_, err = pipe.Exec(ctx)
	if err != nil {
		fmt.Printf("[ERROR] Failed to write state to Redis: %v\n", err)
	}

	// 3. TRIAGE & COGNITION: Only pull history if the heuristic triggers
	if event.Action == "Void" && event.Amount >= 50.0 {
		fmt.Printf("[-] Suspicious Void Detected at %s (%s). Fetching timeline context from Redis...\n", event.VenueID, event.Terminal)
		// Fetch the full sliding window from Redis
		rawTimeline, err := rdb.LRange(ctx, redisKey, 0, -1).Result()
		if err == nil {
			var currentTimeline []POSEvent
			// Redis returns lists in reverse order (newest first).
			// We parse them to feed to the AI.
			for _, rawEvent := range rawTimeline {
				var parsed POSEvent
				json.Unmarshal([]byte(rawEvent), &parsed)
				currentTimeline = append(currentTimeline, parsed)
			}
			// Pass the timeline to the AI (ensure evaluateThreatWithAI accepts []POSEvent)
			aiDecision := evaluateThreatWithAI(ctx, currentTimeline)
			executeMitigation(ctx, aiDecision, event)
		} else {
			fmt.Printf("[ERROR] Failed to fetch timeline from Redis: %v\n", err)
		}
	}

	// Forward ALL events to the C# Analytics Engine asynchronously
	// Using a goroutine prevents network latency from slowing down our Go Sentinel
	go forwardToAnalytics(ctx, event)

	// Tell the generator we received it successfully
	w.WriteHeader(http.StatusOK)
}

// This function packages the context and "sends" it to the AI.
func evaluateThreatWithAI(ctx context.Context, timeline []POSEvent) AIResponse {
	tracer := otel.Tracer("sentinel-soc")
	_, span := tracer.Start(ctx, "evaluateThreatWithAI")
	defer span.End()

	span.SetAttributes(attribute.Int("timeline.length", len(timeline)))

	fmt.Println("\n[!] THRESHOLD BREACHED. Contacting AI Agent...")

	time.Sleep(1 * time.Second) // Simulate network latency

	if len(timeline) >= 3 {
		span.SetAttributes(attribute.String("ai.decision", "revoke_pin"))
		return AIResponse{
			Action: "revoke_pin",
			Reason: "Sweethearting pattern detected.",
		}
	}

	span.SetAttributes(attribute.String("ai.decision", "alert_manager"))
	return AIResponse{
		Action: "alert_manager",
		Reason: "Void activity elevated but temporal pattern does not indicate sweethearting.",
	}

	// Convert the array into a pretty JSON string for the LLM context window
	//timelineBytes, _ := json.MarshalIndent(timeline, "", " ")

	// This is the exact prompt we would send to an LLM API (like OpenAI or Ollama)
	//systemPrompt := fmt.Sprintf(`
	//	You are an autonomous SOC agent.
	//	Analyze the provided chronological transaction timeline for a specific POS terminal.
	//	Identify indicators of internal shrinkage, specifically 'sweethearting' (e.g., an order is placed, time passes, a manager voids the item, and the remaining balance is paid in cash).
	//
	//	If sweethearting is detected, you MUST output the action "revoke_pin".
	//	Otherwise, output "alert_manager".
	//
	//	Context Timeline:
	//	%s`, string(timelineBytes))
	//end systemPrompt

	//fmt.Printf(">> SYSTEM PROMPT SENT TO LLM:\n%s\n\n", systemPrompt)

	// Simulated AI Logic: If we see a timeline with multiple events ending in a void, it flags it.
	// TODO: fetch to anthropic
}

func executeMitigation(ctx context.Context, response AIResponse, triggerEvent POSEvent) {
	tracer := otel.Tracer("sentinel-soc")
	ctx, span := tracer.Start(ctx, "executeMitigation")
	defer span.End()
	span.SetAttributes(attribute.String("mitigation.action", response.Action))
	fmt.Printf("[AGENT] Decision Reached: -> %s\n", response.Action)
	fmt.Printf("[REASONING] %s\n", response.Reason)

	switch response.Action {
	case "revoke_pin":
		fmt.Printf("[CRITICAL ACTION] Sweethearting detected. Revoking POS PIN at %s.\n", triggerEvent.VenueID)
	case "alert_manager":
		fmt.Printf("[WARNING] Paging floor manager to review Terminal %s.\n", triggerEvent.Terminal)
	}
}

// forwardToAnalytics pushes the evaluated event downstream to the C# API
func forwardToAnalytics(ctx context.Context, event POSEvent) {
	tracer := otel.Tracer("sentinel-soc")
	// Use context.Background() as the base here because this is a goroutine that outlives the HTTP request,
	// but link it to the original trace context.
	ctx, span := tracer.Start(context.Background(), "ForwardToAnalytics", trace.WithLinks(trace.LinkFromContext(ctx)))
	defer span.End()

	analyticsURL := os.Getenv("ANALYTICS_URL")
	if analyticsURL == "" {
		fmt.Printf("[ERROR] ANALYTICS_URL is not set. Data dropped.")
		return
	}

	// Serialize the event back into JSON
	payloadBytes, err := json.Marshal(event)
	if err != nil {
		fmt.Printf("[ERROR] Failed to marshal event for forwarding: %v\n", err)
		return
	}

	maxRetries := 4
	baseDelay := 1 * time.Second

	// The Exponential Backoff Loop
	for attempt := 0; attempt <= maxRetries; attempt++ {
		req, err := http.NewRequest("POST", analyticsURL, bytes.NewBuffer(payloadBytes))
		if err == nil {
			req.Header.Set("Content-Type", "application/json")
			client := &http.Client{Timeout: 5 * time.Second}
			resp, err := client.Do(req)

			// If the network call succeeded AND the C# API accepted it
			if err == nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
				resp.Body.Close()
				// Success! Exit the function.
				return
			}

			// If we got a response but it was a 5xx Server Error, close the body to prevent memory leaks
			if resp != nil {
				resp.Body.Close()
			}
		}

		// If we reached the max retries, give up to prevent hanging the system forever
		if attempt == maxRetries {
			fmt.Printf("[FATAL] Analytics Engine unreachable after %d attempts. Payload dropped: %s\n", maxRetries, event.EventID)
			return
		}

		// Calculate the backoff delay: 1s, 2s, 4s, 8s...
		delay := baseDelay * time.Duration(1<<attempt)
		fmt.Printf("[WARNING] Analytics Engine unreachable. Retrying in %v (Attempt %d/%d)...\n", delay, attempt+1, maxRetries)
		time.Sleep(delay)
	}
}

func main() {
	//Initialise Otel Tracer
	tp := initTracer()
	defer func() {
		if err := tp.Shutdown(context.Background()); err != nil {
			fmt.Printf("Error shutting down tracer provider: %v", err)
		}
	}()

	// Initialise our Redis connection before starting the HTTP server
	initRedis()
	fmt.Println("Redis Engine initiated.")
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
