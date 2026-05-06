package provider

import "context"

// BillingProvider is our cloud-agnostic contract
// Any struct that implements these two methods can be user by our sre bot
type BillingProvider interface {
	GetCurrentMonthSpend(ctx context.Context, targetResourceGroup string) (float64, error)
	ScaleToZero(ctx context.Context, resourceGroup string, targetApps []string) error
}
