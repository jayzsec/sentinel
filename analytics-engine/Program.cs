using Scalar.AspNetCore;
using System.Collections.Concurrent;
using AnalyticsEngine.Models;
using Microsoft.AspNetCore.Mvc;

var builder = WebApplication.CreateBuilder(args);

// Add services to the container.
builder.Services.AddEndpointsApiExplorer();
builder.Services.AddOpenApi();

var app = builder.Build();

// Configure the HTTP request pipeline.
if (app.Environment.IsDevelopment())
{
    app.MapOpenApi();
    app.MapScalarApiReference();
}

// This thread-safe dictionary tracks live stats for each terminal
var terminalStats = new ConcurrentDictionary<string, TerminalMetrics>();

app.UseHttpsRedirection();

// 1. The INGEST Endpoint (Where Go sends the data)
app.MapPost("/ingest", ([FromBody] POSEvent posEvent) =>
{
    // Get the current stats for this terminal, or create a new entry if it's the first time we've seen it
    var stats = terminalStats.GetOrAdd(posEvent.Terminal, new TerminalMetrics());

    // Update our numbers based on the action
    stats.TotalTransactions++;

    if (posEvent.Action == "Void")
    {
        stats.TotalVoids += posEvent.Amount;
    }
    else if (posEvent.Action == "Payment" || posEvent.Action == "Order")
    {
        stats.TotalRevenue += posEvent.Amount;
    }

    return Results.Ok(new { status = "Ingested", eventId = posEvent.EventID });
});

// 2. The DASHBOARD Endpoint (Where the Manager views the data)
app.MapGet("/dashboard", () =>
{
    // LINQ (Language Integrated Query) makes calculating venue-wide totals incredibly easy
    var venueTotalRevenue = terminalStats.Values.Sum(t => t.TotalRevenue);
    var venueTotalVoids = terminalStats.Values.Sum(t => t.TotalVoids);

    // Return a rich JSON object for the front-end dashboard
    return Results.Ok(new
    {
        VenueTotalRevenue = venueTotalRevenue,
        VenueTotalVoids = venueTotalVoids,
        ActiveTerminal = terminalStats.Count,
        TerminalBreakdown = terminalStats
    });
});

app.Run();

// --- Helper Class for tracking metrics ---
public class TerminalMetrics
{
    public int TotalTransactions { get; set; } = 0;
    public double TotalRevenue { get; set; } = 0;
    public double TotalVoids { get; set; } = 0;
}
