package provider

import (
	"context"
	"fmt"
	"log"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore/to"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appcontainers/armappcontainers/v2"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/costmanagement/armcostmanagement"
)

// AzureProvider holds the state and credentials needed for Azure.
type AzureProvider struct {
	SubscriptionID string
}

// NewAzureProvider acts as a constructor for our Azure adapter.
func NewAzureProvider(subID string) *AzureProvider {
	return &AzureProvider{
		SubscriptionID: subID,
	}
}

// GetCurrentMonthSpend satisfies the BillingProvider interface.
func (a *AzureProvider) GetCurrentMonthSpend(ctx context.Context, targetResourceGroup string) (float64, error) {
	fmt.Printf("[Azure SDK] Authenticating via DefaultAzureCredential...\n")
	fmt.Printf("[Azure SDK] Querying Cost Management API for Resource Group: %s\n", targetResourceGroup)

	// DONE: Replace with exact github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/consumption/armconsumption logic.
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return 0, fmt.Errorf("[+] Authentication failed: %v", err)
	}

	client, err := armcostmanagement.NewQueryClient(cred, nil)
	if err != nil {
		return 0, fmt.Errorf("[+] Failed to create cost client: %v", err)
	}

	scope := fmt.Sprintf("subscriptions/%s", a.SubscriptionID)
	// Define what we are asking Azure for: Actual Cost, Month-to-Date
	query := armcostmanagement.QueryDefinition{
		Type:      to.Ptr(armcostmanagement.ExportTypeActualCost),
		Timeframe: to.Ptr(armcostmanagement.TimeframeTypeMonthToDate),
		Dataset: &armcostmanagement.QueryDataset{
			Granularity: to.Ptr(armcostmanagement.GranularityTypeDaily),
			Aggregation: map[string]*armcostmanagement.QueryAggregation{
				"totalcost": {
					Name:     to.Ptr("PreTaxCost"),
					Function: to.Ptr(armcostmanagement.FunctionTypeSum),
				},
			},
		},
	}

	resp, err := client.Usage(ctx, scope, query, nil)
	if err != nil {
		return 0, fmt.Errorf("Failed to query usage: %v", err)
	}

	// Parse the messy JSON response from Azure billing
	var totalSpend float64 = 0.0
	if resp.Properties != nil && resp.Properties.Rows != nil {
		for _, row := range resp.Properties.Rows {
			// Cost is usually the first column in the returned row array
			if val, ok := row[0].(float64); ok {
				totalSpend += val
			}
		}
	}

	return totalSpend, nil

	// We simulate a cost of $12.50 to test our SRE mitigation logic (since our budget is $10.00).
	//simulatedCost := 12.50
	//return simulatedCost, nil
}

// ScaleToZero satisfies the BillingProvider interface.
func (a *AzureProvider) ScaleToZero(ctx context.Context, resourceGroup string, targetApps []string) error {
	fmt.Printf("[Azure SDK] Authenticating via DefaultAzureCredential...\n")
	fmt.Printf("[Azure SDK] CRITICAL: Updating Container App '%s' to 0 replicas...\n", targetApps)

	// DONE: Replace with github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/appcontainers/armappcontainers logic.
	cred, err := azidentity.NewDefaultAzureCredential(nil)
	if err != nil {
		return fmt.Errorf("[+] Authentication failed: %v", err)
	}

	client, err := armappcontainers.NewContainerAppsClient(a.SubscriptionID, cred, nil)
	if err != nil {
		return fmt.Errorf("[+] Failed to create container apps client: %v", err)
	}

	for _, appName := range targetApps {
		fmt.Printf("[FINOPS] Initiating Scale-to-Zero for %s...\n", appName)

		// 1. Get the current app configuration
		app, err := client.Get(ctx, resourceGroup, appName, nil)
		if err != nil {
			log.Printf("[ERROR] Could not find app %s: %v\n", appName, err)
			continue
		}

		// 2. Modify the template to force Min and Max replicas to 0
		app.Properties.Template.Scale.MinReplicas = to.Ptr[int32](0)
		app.Properties.Template.Scale.MaxReplicas = to.Ptr[int32](0)

		// 3. Push the update back to Azure
		poller, err := client.BeginUpdate(ctx, resourceGroup, appName, app.ContainerApp, nil)
		if err != nil {
			log.Printf("[ERROR] Failed to send update for %s: %v\n", appName, err)
			continue
		}

		// Wait for Azure to finish scaling down
		_, err = poller.PollUntilDone(ctx, nil)
		if err != nil {
			log.Printf("[ERROR] Scale down timed out for %s: %v\n", appName, err)
		} else {
			fmt.Printf("[FINOPS] Successfully neutralized %s. Billing stopped.\n", appName)
		}
	}
	return nil
}
