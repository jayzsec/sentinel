using Microsoft.Azure.Cosmos;
using System.Text.Json.Serialization;
using Newtonsoft.Json;
using System.Net;
using Azure.Monitor.OpenTelemetry.AspNetCore;

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
builder.Services.AddSingleton<Container>(sp =>
{
    CosmosClient cosmosClient = new CosmosClient(endpointUri, primaryKey, new CosmosClientOptions()
    {
        SerializerOptions = new CosmosSerializationOptions()
        {
            PropertyNamingPolicy = CosmosPropertyNamingPolicy.CamelCase
        }
    });
    
    // Auto-create the database and container if they don't exist (Great for dev environments)
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