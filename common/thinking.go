package common

// GetThinkLevel maps budget_tokens to effort level
// Based on CCR's implementation
func GetThinkLevel(budgetTokens int) string {
	if budgetTokens <= 0 {
		return "low"
	}
	
	// Map token budget to effort levels
	// low: < 2000 tokens
	// medium: 2000-8000 tokens  
	// high: > 8000 tokens
	if budgetTokens < 2000 {
		return "low"
	} else if budgetTokens < 8000 {
		return "medium"
	}
	return "high"
}
