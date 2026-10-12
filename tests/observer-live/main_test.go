package main

import (
	"github.com/iamrobertduda/Zwoice/internal/proximity"
	"testing"
)

func TestDistributionDoesNotCountRasterCornersAsSeparateGroups(t *testing.T) {
	positions := []proximity.Position{{Course: 1, X: 299, Y: 299}, {Course: 1, X: 301, Y: 299}, {Course: 1, X: 299, Y: 301}, {Course: 1, X: 301, Y: 301}}
	if separatedGroups(positions) != 1 {
		t.Fatal("nearby riders falsely qualified as distributed")
	}
	positions = append(positions, proximity.Position{Course: 2}, proximity.Position{Course: 1, X: 1000}, proximity.Position{Course: 1, Event: 5})
	if separatedGroups(positions) != 4 {
		t.Fatal("worlds, events or geographically separated riders not counted")
	}
}
