package engine

import (
	"maps"
	"slices"
	"strconv"
	"strings"

	"github.com/mattwilkinsonn/tessera/internal/config"
	"github.com/mattwilkinsonn/tessera/internal/wm"
)

// SlotKey identifies one occurrence of a configured window name on a display.
type SlotKey string

// WindowRef records the last window that filled a slot.
type WindowRef struct {
	ID       int    `json:"id"`
	App      string `json:"app"`
	Displays string `json:"displays"`
	Seq      uint64 `json:"seq"`
}

// SlotMemory is the persisted slot history shared by layout modes.
type SlotMemory struct {
	Seq   uint64                `json:"seq"`
	Slots map[SlotKey]WindowRef `json:"slots"`
}

// SlotKeys returns stable occurrence keys for names in one display layout.
func SlotKeys(mode string, display config.DisplayName, names []string) []SlotKey {
	keys := make([]SlotKey, len(names))
	occurrences := make(map[string]int, len(names))
	for index, name := range names {
		occurrences[name]++
		keys[index] = SlotKey(mode + ":" + string(display) + "/" + name + "#" + strconv.Itoa(occurrences[name]))
	}
	return keys
}

// Reserve selects the current display set's remembered owner for each window id.
func Reserve(profile *config.Profile, memory SlotMemory, keys []SlotKey, displays string, windows []wm.Window) map[int]SlotKey {
	reserved := make(map[int]SlotKey)
	if profile == nil {
		return reserved
	}
	live := make(map[int]wm.Window, len(windows))
	for _, window := range windows {
		live[window.ID] = window
	}
	owners := make(map[int]WindowRef)
	for _, key := range keys {
		ref, exists := memory.Slots[key]
		if !exists {
			continue
		}
		name, valid := slotName(key)
		if !valid {
			continue
		}
		spec, exists := profile.Windows[name]
		if !exists {
			continue
		}
		window, exists := live[ref.ID]
		if !exists || window.App != ref.App || !matchesSpec(spec, window.App, window.Title) {
			continue
		}
		owner, claimed := owners[ref.ID]
		if claimed && !preferredOwner(ref, owner, displays) {
			continue
		}
		owners[ref.ID] = ref
		reserved[ref.ID] = key
	}
	return reserved
}

// PruneSlots drops entries whose window or configured spec no longer matches.
func PruneSlots(profile *config.Profile, memory SlotMemory, windows []wm.Window) SlotMemory {
	pruned := SlotMemory{Seq: memory.Seq, Slots: make(map[SlotKey]WindowRef)}
	if profile == nil {
		return pruned
	}
	live := make(map[int]wm.Window, len(windows))
	for _, window := range windows {
		live[window.ID] = window
	}
	for key, ref := range memory.Slots {
		name, valid := slotName(key)
		if !valid {
			continue
		}
		spec, exists := profile.Windows[name]
		if !exists {
			continue
		}
		window, exists := live[ref.ID]
		if exists && window.App == ref.App && matchesSpec(spec, window.App, window.Title) {
			pruned.Slots[key] = ref
		}
	}
	return pruned
}

// RecordSlots updates only filled keys and gives the run one new sequence number.
func RecordSlots(memory SlotMemory, filled map[SlotKey]int, displays string, windows []wm.Window) SlotMemory {
	recorded := SlotMemory{Seq: memory.Seq + 1, Slots: make(map[SlotKey]WindowRef, len(memory.Slots)+len(filled))}
	maps.Copy(recorded.Slots, memory.Slots)
	live := make(map[int]wm.Window, len(windows))
	for _, window := range windows {
		live[window.ID] = window
	}
	for key, id := range filled {
		window, exists := live[id]
		if !exists {
			continue
		}
		recorded.Slots[key] = WindowRef{ID: id, App: window.App, Displays: displays, Seq: recorded.Seq}
	}
	return recorded
}

func preferredOwner(candidate, current WindowRef, displays string) bool {
	candidateMatches := candidate.Displays == displays
	currentMatches := current.Displays == displays
	if candidateMatches != currentMatches {
		return candidateMatches
	}
	return candidate.Seq > current.Seq
}

func slotName(key SlotKey) (string, bool) {
	value := string(key)
	separator := strings.IndexByte(value, ':')
	slotStart := strings.IndexByte(value, '/')
	numberStart := strings.LastIndexByte(value, '#')
	if separator <= 0 || slotStart <= separator || numberStart <= slotStart+1 || numberStart == len(value)-1 {
		return "", false
	}
	occurrence, err := strconv.Atoi(value[numberStart+1:])
	if err != nil || occurrence < 1 {
		return "", false
	}
	return value[slotStart+1 : numberStart], true
}

// DisplaySet returns sorted profile display names for the connected topology.
func DisplaySet(profile *config.Profile, displays []wm.Display) string {
	if profile == nil {
		return ""
	}
	names := make([]string, 0, len(displays))
	for _, display := range displays {
		if name, ok := displayNameForWidth(profile, display.Frame.W); ok {
			names = append(names, string(name))
		}
	}
	slices.Sort(names)
	return strings.Join(names, ",")
}
