package core

import "math"

// ComputeRotationCards scores already adjusted, ascending, as-of input panels.
// It shares the signal calculator with the bot; it never fetches or stores data.
func ComputeRotationCards(panels map[string][]DailyBarAdj, quantileWindow int) []SignalCard {
	c := NewSignalComputer(nil, nil, quantileWindow, nil)
	cards := make([]SignalCard, 0, len(RotationCodes))
	for _, code := range RotationCodes {
		card := SignalCard{TsCode: code, Score: math.NaN(), YZVol: math.NaN(), Quantile: math.NaN(), Weight: 1}
		c.scoreFromSeries(&card, panels[code])
		cards = append(cards, card)
	}
	rankCards(cards)
	return cards
}
