using System.Text.Json.Serialization;

namespace AnalyticsEngine.Models;

public class POSEvent
{
 [JsonPropertyName("event_id")]
 public string EventID { get; set; } = string.Empty;
 [JsonPropertyName("terminal")]
 public string Terminal { get; set; } = string.Empty;
 [JsonPropertyName("staff_role")]
 public string StaffRole { get; set; } = string.Empty;
 [JsonPropertyName("action")]
 public string Action { get; set; } = string.Empty;
 [JsonPropertyName("amount")]
 public double Amount { get; set; }
 [JsonPropertyName("timestamp")]
 public DateTime Timestamp { get; set; }
}