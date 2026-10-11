// Package yabai implements the wm contract with yabai's command-line interface.
package yabai

import (
	"fmt"
	"math"
	"strconv"

	"github.com/mattwilkinsonn/tessera/internal/wm"
)

type RuleAdd struct {
	Label   string
	App     string
	Subrole string
	Display *int
	Space   *int
	Manage  *bool
}

type BspStep struct {
	Args       []string
	Settle     int
	Unfloat    int
	HasUnfloat bool
}

func QueryWindowsArgs() []string { return []string{"-m", "query", "--windows"} }

func QueryWindowsOnSpaceArgs(spaceIndex int) []string {
	return []string{"-m", "query", "--windows", "--space", strconv.Itoa(spaceIndex)}
}

func QueryWindowArgs(windowID int) []string {
	return []string{"-m", "query", "--windows", "--window", strconv.Itoa(windowID)}
}

func QueryFocusedWindowArgs() []string { return []string{"-m", "query", "--windows", "--window"} }

func QuerySpacesArgs() []string { return []string{"-m", "query", "--spaces"} }

func QueryFocusedSpaceArgs() []string { return []string{"-m", "query", "--spaces", "--space"} }

func QueryDisplaysArgs() []string { return []string{"-m", "query", "--displays"} }

func CreateSpaceArgs() []string { return []string{"-m", "space", "--create"} }

func DestroySpaceArgs(spaceIndex int) []string {
	return []string{"-m", "space", strconv.Itoa(spaceIndex), "--destroy"}
}

func LabelSpaceArgs(spaceIndex int, label string) []string {
	return []string{"-m", "space", strconv.Itoa(spaceIndex), "--label", label}
}

func SetSpaceLayoutArgs(spaceIndex int, layout wm.Layout) []string {
	return []string{"-m", "space", strconv.Itoa(spaceIndex), "--layout", string(layout)}
}

func MoveSpaceToIndexArgs(spaceIndex, toIndex int) []string {
	return []string{"-m", "space", strconv.Itoa(spaceIndex), "--move", strconv.Itoa(toIndex)}
}

func BalanceSpaceArgs(spaceIndex *int) []string {
	if spaceIndex == nil {
		return []string{"-m", "space", "--balance"}
	}
	return []string{"-m", "space", strconv.Itoa(*spaceIndex), "--balance"}
}

func MoveWindowToSpaceArgs(windowID, spaceIndex int) []string {
	return []string{"-m", "window", strconv.Itoa(windowID), "--space", strconv.Itoa(spaceIndex)}
}

func MoveWindowToDisplayArgs(windowID int, selector wm.DisplaySelector) []string {
	return []string{"-m", "window", strconv.Itoa(windowID), "--display", displaySelectorArg(selector)}
}

func SetSplitRatioArgs(windowID int, absRatio float64) []string {
	return SetSplitRatioArgArgs(windowID, "abs:"+strconv.FormatFloat(absRatio, 'f', -1, 64))
}

func SetSplitRatioArgArgs(windowID int, ratio string) []string {
	return []string{"-m", "window", strconv.Itoa(windowID), "--ratio", ratio}
}

func ToggleSplitArgs(windowID int) []string {
	return []string{"-m", "window", strconv.Itoa(windowID), "--toggle", "split"}
}

func ToggleFloatArgs(windowID int) []string {
	return []string{"-m", "window", strconv.Itoa(windowID), "--toggle", "float"}
}

func ArmInsertArgs(windowID int, direction string) []string {
	return []string{"-m", "window", strconv.Itoa(windowID), "--insert", direction}
}

func SwapWindowsArgs(direction wm.Direction) []string {
	return []string{"-m", "window", "--swap", string(direction)}
}

func WarpWindowArgs(direction wm.Direction) []string {
	return []string{"-m", "window", "--warp", string(direction)}
}

func ResizeWindowArgs(edge wm.ResizeEdge, dx, dy int) []string {
	return []string{"-m", "window", "--resize", fmt.Sprintf("%s:%d:%d", edge, dx, dy)}
}

func FocusWindowArgs(windowID int) []string {
	return []string{"-m", "window", "--focus", strconv.Itoa(windowID)}
}

func FocusWindowDirArgs(selector wm.FocusSelector) []string {
	return []string{"-m", "window", "--focus", string(selector)}
}

func FocusDisplayArgs(selector wm.DisplaySelector) []string {
	return []string{"-m", "display", "--focus", displaySelectorArg(selector)}
}

func RuleListArgs() []string { return []string{"-m", "rule", "--list"} }

func RuleRemoveArgs(label string) []string { return []string{"-m", "rule", "--remove", label} }

func RuleAddArgs(rule RuleAdd) []string {
	args := []string{"-m", "rule", "--add", "label=" + rule.Label}
	if rule.App != "" {
		args = append(args, "app="+rule.App)
	}
	if rule.Subrole != "" {
		args = append(args, "subrole="+rule.Subrole)
	}
	if rule.Display != nil {
		args = append(args, "display="+strconv.Itoa(*rule.Display))
	}
	if rule.Space != nil {
		args = append(args, "space="+strconv.Itoa(*rule.Space))
	}
	if rule.Manage != nil {
		manage := "off"
		if *rule.Manage {
			manage = "on"
		}
		args = append(args, "manage="+manage)
	}
	return args
}

func RuleApplyArgs() []string { return []string{"-m", "rule", "--apply"} }

func SignalAddArgs(event wm.Event, label, action string) []string {
	return []string{"-m", "signal", "--add", "event=" + string(event), "label=" + label, "action=" + action}
}

func displaySelectorArg(selector wm.DisplaySelector) string {
	if selector.Position != "" {
		return string(selector.Position)
	}
	return strconv.Itoa(selector.Index)
}

func RatioArg(ratio float64, warn func(string)) string {
	clamped := math.Max(0.1, math.Min(0.9, ratio))
	if clamped != ratio && warn != nil {
		warn(fmt.Sprintf("ratio %v clamped to %.4f", ratio, clamped))
	}
	return "abs:" + strconv.FormatFloat(clamped, 'f', 4, 64)
}

func BSPSteps(spaceIndex int, target wm.SpaceLayoutTarget, warn func(string)) []BspStep {
	tracks := make([][]int, 0, len(target.Tracks))
	for _, track := range target.Tracks {
		if len(track) != 0 {
			tracks = append(tracks, track)
		}
	}
	anchors := make([]int, 0, len(tracks))
	for _, track := range tracks {
		anchors = append(anchors, track[0])
	}
	if len(anchors) == 0 {
		return nil
	}
	direction := "east"
	if target.Kind == wm.LayoutRows {
		direction = "south"
	}
	steps := []BspStep{{Args: SetSpaceLayoutArgs(spaceIndex, wm.LayoutBSP)}}
	for index, anchor := range anchors {
		steps = append(steps, BspStep{Args: MoveWindowToSpaceArgs(anchor, spaceIndex), Settle: 400, Unfloat: anchor, HasUnfloat: true})
		if index < len(anchors)-1 {
			steps = append(steps, BspStep{Args: ArmInsertArgs(anchor, direction), Settle: 150})
		}
	}
	weights := target.Weights
	if weights == nil {
		weights = make([]float64, len(anchors))
		for index := range weights {
			weights[index] = 1
		}
	}
	for index, ratio := range chainRatios(weights) {
		if index >= len(anchors) {
			continue
		}
		stepWarn := func(line string) {
			if warn != nil {
				warn(fmt.Sprintf("space %d: %s", spaceIndex, line))
			}
		}
		steps = append(steps, BspStep{Args: SetSplitRatioArgArgs(anchors[index], RatioArg(ratio, stepWarn)), Settle: 150})
	}
	for _, track := range tracks {
		anchor := track[0]
		for _, extra := range track[1:] {
			if extra == anchor {
				continue
			}
			steps = append(steps,
				BspStep{Args: ArmInsertArgs(anchor, "stack"), Settle: 150},
				BspStep{Args: MoveWindowToSpaceArgs(extra, spaceIndex), Settle: 350, Unfloat: extra, HasUnfloat: true},
			)
		}
	}
	return steps
}

func chainRatios(weights []float64) []float64 {
	if len(weights) < 2 {
		return nil
	}
	total := 0.0
	maximum := 0.0
	for _, weight := range weights {
		total += weight
		maximum = math.Max(maximum, weight)
	}
	scaled := weights
	if math.IsInf(total, 0) {
		scale := math.Ldexp(1, -int(math.Ceil(math.Log2(maximum))))
		scaled = make([]float64, len(weights))
		for index, weight := range weights {
			scaled[index] = weight * scale
		}
		total = 0
		for _, weight := range scaled {
			total += weight
		}
	}
	ratios := make([]float64, 0, len(weights)-1)
	for _, weight := range scaled[:len(scaled)-1] {
		ratios = append(ratios, weight/total)
		total -= weight
	}
	return ratios
}
