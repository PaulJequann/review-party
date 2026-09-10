package main

import (
	"context"

	"reviewparty/internal/configurationhub" //nolint:depguard // Cobra composes the Hub adapter with a store-backed receipt provider here, beside the existing Hub wiring.
	"reviewparty/internal/engine"
	"reviewparty/internal/store"
)

// receiptHistoryLimit bounds the recent runs behind one profile receipt.
const receiptHistoryLimit = 5

// profileReceiptProvider returns execution receipts from recorded history.
// A nil provider renders profiles exactly as before; per-profile misses
// simply omit the receipt line.
func profileReceiptProvider(ctx context.Context, configurationPath string) configurationhub.ReceiptProvider {
	conductor, err := engine.New(engine.Config{UserConfigurationPath: configurationPath})
	if err != nil {
		return nil
	}
	return func(profile string) (configurationhub.ProfileReceipt, bool) {
		page, err := conductor.History(ctx, store.HistoryQuery{Profile: profile, Limit: receiptHistoryLimit})
		if err != nil {
			return configurationhub.ProfileReceipt{}, false
		}
		return receiptFromSummary(store.SummarizeHistory(page), profile)
	}
}

func receiptFromSummary(summary store.HistorySummary, profile string) (configurationhub.ProfileReceipt, bool) {
	for _, entry := range summary.Profiles {
		if entry.Profile != profile {
			continue
		}
		return configurationhub.ProfileReceipt{
			Runs: entry.Runs, MedianDuration: entry.MedianDuration, TotalFindings: entry.TotalFindings,
			IncompleteRuns: entry.IncompleteRuns,
		}, true
	}
	return configurationhub.ProfileReceipt{}, false
}

// profileReceiptWarning renders one plan warning from recorded runs.
func profileReceiptWarning(ctx context.Context, configurationPath, profile string) string {
	provider := profileReceiptProvider(ctx, configurationPath)
	if provider == nil {
		return ""
	}
	receipt, found := provider(profile)
	if !found {
		return ""
	}
	return "profile \"" + profile + "\": " + configurationhub.FormatReceipt(receipt) + " (history --profile " + profile + " for detail)"
}
