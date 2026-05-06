package main

import (
	"context"
	"finops-bot/provider"
	"fmt"
	"os"
)

func main() {
	// 1. Configuration Setup
	cloud := os.Getenv("CLOUD_PROVIDER")
	if cloud == "" {
		cloud = "azure" // Defaulting to Azure for our current infrastructure
	}

	// Our interface variable that will hold whichever provider we initialize
	var bot provider.BillingProvider

	// 2. The Factory: Injecting the specific cloud adapter
	switch cloud {
	case "azure":
		subID := os.Getenv("AZURE_SUBSCRIPTION_ID")
		bot = provider.NewAzureProvider(subID)
	case "aws":
		// drop in your existing AWS Cost Explorer logic aws jayzsec/cost-tracker !
		// bot = provider.NewAWSProvider()
		fmt.Println("AWS adapter not yet wired.")
		os.Exit(1)
	default:
		fmt.Printf("Unknown cloud provider: %s\n", cloud)
		os.Exit(1)
	}

	ctx := context.Background()
	targetRG := "rg-hospitality-capstone"
	//targetApp := "analytics-engine"
	// Define the cluster we want to neutralize if the budget is blown
	appsToShutdown := []string{"pos-generator", "sentinel-soc", "analytics-engine"}
	budgetLimit := 10.0 // strict $10 FinOps constraint

	fmt.Println("========================================")
	fmt.Printf("Active FinOps Sentinel Online (%s)\n", cloud)
	fmt.Println("========================================")

	// 3. The Active FinOps Reasoning Loop
	spend, err := bot.GetCurrentMonthSpend(ctx, targetRG)
	if err != nil {
		fmt.Printf("FATAL: Failed to fetch billing data: %s\n", err)
		os.Exit(1)
	}

	fmt.Printf("Current monthly spend for '%s': $%.2f (Limit: $%.2f)\n", targetRG, spend, budgetLimit)

	// 4. The Mitigation Action
	if spend >= budgetLimit {
		fmt.Println("\n[!] FINANCIAL THRESHOLD BREACHED.")
		fmt.Println("[!] Executing automated infrastructure mitigation...")

		err := bot.ScaleToZero(ctx, targetRG, appsToShutdown)
		if err != nil {
			fmt.Printf("FATAL: Failed to scale application: %s\n", err)
			os.Exit(1)
		}

		fmt.Println("[+] Mitigation successful. Vulnerable compute nodes isolated to stop billing.")
	} else {
		fmt.Println("[+] Spend is within normal parameters. Terminating check.")
	}
}
