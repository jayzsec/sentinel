using Microsoft.Azure.Cosmos;
using System.Text.Json.Serialization;
using Newtonsoft.Json;
using System.Net;
using Azure.Monitor.OpenTelemetry.AspNetCore;
using Microsoft.AspNetCore.Mvc;

var builder = WebApplication.CreateBuilder(args);

// 1. COSMOS DB CONFIGURATION
// In a production environment, these would be pulled from Azure Key Vault or Environment Variables.
string appInsightsString = Environment.GetEnvironmentVariable("APPLICATIONINSIGHTS_CONNECTION_STRING") ?? string.Empty;
string endpointUri = builder.Configuration["CosmosDb:EndpointUri"] ?? Environment.GetEnvironmentVariable("COSMOS_ENDPOINT") ?? throw new InvalidOperationException("Cosmos Endpoint missing");
string primaryKey = builder.Configuration["CosmosDb:PrimaryKey"] ?? Environment.GetEnvironmentVariable("COSMOS_KEY") ?? throw new InvalidOperationException("Cosmos Key missing");
string databaseId = "SentinelDB";
string containerId = "POSEvents";

// Enable OTel and route it to App Insights.
// It will automatically read the APPLICATIONINSIGHTS_CONNECTION_STRING from the environment.
if (!string.IsNullOrEmpty(appInsightsString))
{
    // Cloud mode: Enable OTel and route to Azure Application Insights
    builder.Services.AddOpenTelemetry().UseAzureMonitor();
    Console.WriteLine("[+] Application Insights Telemetry Enabled.");
}
else
{
    // Local mode: Skip OTel so the app doesn't crash on desktop
    Console.WriteLine("[-] APPLICATIONINSIGHTS_CONNECTION_STRING missing. Telemetry disabled for local run.");
}

// 2. DEPENDENCY INJECTION
// We register the Cosmos DB Container as a Singleton so we don't exhaust SNAT ports by creating a new client per request.
// FIX 1: Register the CosmosClient itself so [FromServices] can find it.
// FIX 2: Add ConnectionMode.Gateway to fix the 408 Timeout.
builder.Services.AddSingleton<CosmosClient>(sp =>
{
    return new CosmosClient(endpointUri, primaryKey, new CosmosClientOptions()
    {
        ConnectionMode = ConnectionMode.Gateway,
        SerializerOptions = new CosmosSerializationOptions()
        {
            PropertyNamingPolicy = CosmosPropertyNamingPolicy.CamelCase
        }
    });
});

// Register the Container by pulling the registered CosmosClient
builder.Services.AddSingleton<Container>(sp =>
{
    var cosmosClient = sp.GetRequiredService<CosmosClient>();
    Database database = cosmosClient.CreateDatabaseIfNotExistsAsync(databaseId).GetAwaiter().GetResult();
    return database.CreateContainerIfNotExistsAsync(containerId, "/venue_id").GetAwaiter().GetResult();
});

var app = builder.Build();

// 3. THE INGESTION ENDPOINT (Called by the Go Sentinel)
app.MapPost("/ingest", async (POSEvent incomingEvent, Container container) =>
{
    try
    {
        // Write the document to Cosmos DB, physically partitioning it by VenueID for maximum performance.
        await container.CreateItemAsync(
            item: incomingEvent,
            partitionKey: new PartitionKey(incomingEvent.VenueID)
        );

        Console.WriteLine($"[+] Persisted Event {incomingEvent.EventID} to Cosmos DB ({incomingEvent.VenueID}).");
        return Results.Ok(new { status = "persisted", id = incomingEvent.EventID });
    }
    catch (CosmosException ex) when (ex.StatusCode == HttpStatusCode.Conflict)
    {
        // Exponential Backoff might send the same event twice. We safely ignore duplicates.
        Console.WriteLine($"[-] Event {incomingEvent.EventID} already exists. Ignoring duplicate.");
        return Results.Ok(); 
    }
    catch (Exception ex)
    {
        Console.WriteLine($"[FATAL] Database write failed: {ex.Message}");
        return Results.Problem("Failed to write to database.", statusCode: 500);
    }
});

// 4. THE DASHBOARD ENDPOINT (Called by Power BI)
app.MapGet("/dashboard/{venueId}", async (string venueId, Container container) =>
{
    Console.WriteLine($"[i] Power BI requested dashboard data for Venue: {venueId}");
    
    // Define the SQL query scoped strictly to the partition key
    var sqlQueryText = "SELECT * FROM c WHERE c.venue_id = @venueId ORDER BY c.timestamp DESC OFFSET 0 LIMIT 100";
    
    var queryDefinition = new QueryDefinition(sqlQueryText)
        .WithParameter("@venueId", venueId);

    using var queryResultSetIterator = container.GetItemQueryIterator<POSEvent>(
        queryDefinition,
        requestOptions: new QueryRequestOptions { PartitionKey = new PartitionKey(venueId) }
    );

    var events = new List<POSEvent>();

    while (queryResultSetIterator.HasMoreResults)
    {
        var currentResultSet = await queryResultSetIterator.ReadNextAsync();
        foreach (var posEvent in currentResultSet)
        {
            events.Add(posEvent);
        }
    }

    return Results.Ok(events);
});

// 5. THE API for index.html
//app.MapGet("/api/events/latest", async (CosmosClient cosmos, ILogger<Program> logger) =>

// Add [FromServices] to explicitly declare these are injected dependencies, not HTTP body payloads.
// FIX 3: We now just inject the `Container` directly instead of rebuilding it, ensuring it targets "POSEvents"
// 5. THE API for index.html
app.MapGet("/api/events/latest", async ([FromServices] Container container, [FromServices] ILogger<Program> logger) =>
{
    try
    {
        long yesterdayEpoch = DateTimeOffset.UtcNow.AddDays(-1).ToUnixTimeSeconds();

        // 1. Count Total Events
        var countQuery = new QueryDefinition("SELECT VALUE COUNT(1) FROM c WHERE c._ts >= @yesterday")
            .WithParameter("@yesterday", yesterdayEpoch);
        using var countIterator = container.GetItemQueryIterator<int>(countQuery);
        int events24h = countIterator.HasMoreResults ? (await countIterator.ReadNextAsync()).FirstOrDefault() : 0;

        // 2. Count Active Terminals
        var terminalQuery = new QueryDefinition("SELECT DISTINCT VALUE c.terminal FROM c WHERE c._ts >= @yesterday")
            .WithParameter("@yesterday", yesterdayEpoch);
        using var terminalIterator = container.GetItemQueryIterator<string>(terminalQuery);
        var activeTerminals = new HashSet<string>();
        while (terminalIterator.HasMoreResults)
        {
            var response = await terminalIterator.ReadNextAsync();
            foreach (var terminal in response) activeTerminals.Add(terminal);
        }

        // Notice we just use SELECT * now, because the POSEvent class handles the mapping
         var feedQuery = new QueryDefinition("SELECT TOP 10 * FROM c ORDER BY c._ts DESC");

        // We replace <dynamic> with <POSEvent>
        using var feedIterator = container.GetItemQueryIterator<POSEvent>(feedQuery);

        var recentEvents = new List<POSEvent>();
        while (feedIterator.HasMoreResults)
        {
            recentEvents.AddRange(await feedIterator.ReadNextAsync());
        }

        // 4. The JSON Serializer Bridge & Formatter
        var safeEvents = recentEvents.Select(e => new
        {
            timestamp = e.Timestamp,
            venueId = e.VenueID,
            terminalId = e.Terminal,
            action = string.IsNullOrEmpty(e.Action) ? "System" : e.Action, // Fallback if missing
            amount = $"${e.Amount:0.00}" // No casting required because e.Amount is already a double!
        }).ToList();

        return Results.Ok(new
        {
            activeTerminals = activeTerminals.Count,
            eventsProcessed24h = events24h,
            threatsBlocked = 0,
            latestEvents = safeEvents
        });
    }
    catch (Exception ex)
    {
        logger.LogError(ex, "Failed to retrieve telemetry.");
        return Results.Problem("Telemetry database unreachable.");
    }
});

// Add this right above app.Run();
app.UseDefaultFiles(); // Tells .NET to look for index.html
app.UseStaticFiles();  // Enables serving HTML, CSS, and JS files

app.Run();

// 5. THE DATA MODEL
public class POSEvent
{
    // The Web API catches "event_id" from Go. Cosmos DB saves it as "id".
    [JsonPropertyName("event_id")] 
    [JsonProperty("id")] 
    public string EventID { get; set; } = string.Empty;

    // From here down, both serializers use the exact same snake_case names.
    [JsonPropertyName("venue_id")]
    [JsonProperty("venue_id")]
    public string VenueID { get; set; } = string.Empty;

    [JsonPropertyName("timestamp")]
    [JsonProperty("timestamp")]
    public string Timestamp { get; set; } = string.Empty;

    [JsonPropertyName("terminal")]
    [JsonProperty("terminal")]
    public string Terminal { get; set; } = string.Empty;

    [JsonPropertyName("action")]
    [JsonProperty("action")]
    public string Action { get; set; } = string.Empty;

    [JsonPropertyName("amount")]
    [JsonProperty("amount")]
    public double Amount { get; set; }

    [JsonPropertyName("item_id")]
    [JsonProperty("item_id")]
    public string? ItemID { get; set; }

    [JsonPropertyName("staff_id")]
    [JsonProperty("staff_id")]
    public string StaffID { get; set; } = string.Empty;

    [JsonPropertyName("manager_id")]
    [JsonProperty("manager_id")]
    public string? ManagerID { get; set; }

    [JsonPropertyName("payment_method")]
    [JsonProperty("payment_method")]
    public string? PaymentMethod { get; set; }
}