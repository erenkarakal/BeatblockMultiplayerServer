package lobby

import (
	"fmt"
	"sort"
	"strings"

	"bb-mp-server/internal/protocol"
)

func statusRank(status string) int {
	switch status {
	case statusFinished:
		return 0
	case statusDNF:
		return 1
	case statusDisconnected:
		return 2
	default:
		return 3
	}
}

func rankEntries(round *Round) []protocol.Entry {
	if round == nil {
		return []protocol.Entry{}
	}

	entries := make([]protocol.Entry, 0, len(round.Participants))
	for _, id := range round.Order {
		e := round.Participants[id]
		if e == nil {
			continue
		}

		entry := protocol.Entry{
			ID:       e.ID,
			Name:     e.Name,
			Status:   e.Status,
			PctGrade: e.PctGrade,
			PB:       e.PB,
		}
		if e.Score != nil {
			entry.Grade = e.Score.Grade
			entry.PlusMinus = e.Score.PlusMinus
			entry.Perfects = e.Score.Perfects
			entry.Misses = e.Score.Misses
			entry.Barelies = e.Score.Barelies
			entry.MaxCombo = e.Score.MaxCombo
			entry.MaxHits = e.Score.MaxHits
		}
		entries = append(entries, entry)
	}

	sort.SliceStable(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]

		if statusRank(a.Status) != statusRank(b.Status) {
			return statusRank(a.Status) < statusRank(b.Status)
		}
		if a.PctGrade != b.PctGrade {
			return a.PctGrade > b.PctGrade
		}
		if a.Misses != b.Misses {
			return a.Misses < b.Misses
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})

	return entries
}

func formatGrade(grade, plusMinus string) string {
	switch grade {
	case "perfect":
		return "PERFECT"
	case "what":
		return "???"
	case "":
		return "-"
	}

	out := strings.ToUpper(grade)
	switch plusMinus {
	case "plus":
		out += "+"
	case "minus":
		out += "-"
	}
	return out
}

func formatResults(mapName string, entries []protocol.Entry) string {
	var b strings.Builder
	if mapName == "" {
		mapName = "Unknown map"
	}
	fmt.Fprintf(&b, "=== RESULTS: %s ===\n", mapName)

	place := 0
	var dnf []string
	for _, e := range entries {
		if e.Status != statusFinished {
			dnf = append(dnf, e.Name)
			continue
		}
		place++
		name := e.Name
		if len([]rune(name)) > 18 {
			name = string([]rune(name)[:18])
		}
		mark := "   "
		if e.PB {
			mark = "PB "
		}
		fmt.Fprintf(&b, "%d. %-18s %s%6.2f%% %-8s P %d / M %d / B %d\n",
			place, name, mark, e.PctGrade, formatGrade(e.Grade, e.PlusMinus),
			e.Perfects, e.Misses, e.Barelies)
	}
	if len(dnf) > 0 {
		b.WriteString("DNF: ")
		b.WriteString(strings.Join(dnf, ", "))
	}
	return strings.TrimRight(b.String(), "\n")
}
