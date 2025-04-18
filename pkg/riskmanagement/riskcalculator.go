package riskCalculator

import (
	"fmt"
	"math"
)

func RiskCalculator(accSize float64, riskPerc float64, entry float64, stop float64) float64 {
	riskPerShare := math.Abs(entry - stop)
	maxRiskAmount := accSize * (riskPerc / 100)
	maxSharesToBuy := math.Floor(maxRiskAmount / riskPerShare)
	fmt.Printf("Risk Per Share: %v ", riskPerShare)
	fmt.Printf("Risk Amount: %v ", maxRiskAmount)
	fmt.Printf("Max Shares to Buy: %v ", maxSharesToBuy)
	return maxSharesToBuy
}
