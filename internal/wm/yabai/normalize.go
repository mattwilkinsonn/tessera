package yabai

import (
	"fmt"
	"strconv"

	"github.com/mattwilkinsonn/tessera/internal/wm"
)

type RawYabaiWindow struct {
	ID             int          `json:"id"`
	App            string       `json:"app"`
	Title          string       `json:"title"`
	Display        int          `json:"display"`
	Space          int          `json:"space"`
	HasAXReference bool         `json:"has-ax-reference"`
	IsMinimized    bool         `json:"is-minimized"`
	IsFloating     bool         `json:"is-floating"`
	IsSticky       bool         `json:"is-sticky"`
	IsVisible      bool         `json:"is-visible"`
	SplitType      wm.SplitType `json:"split-type"`
	Frame          wm.Frame     `json:"frame"`
}

type RawYabaiSpace struct {
	ID      int    `json:"id"`
	Index   int    `json:"index"`
	Label   string `json:"label"`
	Display int    `json:"display"`
	Windows []int  `json:"windows"`
	Type    string `json:"type"`
}

type RawYabaiDisplay struct {
	Index  int      `json:"index"`
	Frame  wm.Frame `json:"frame"`
	Spaces []int    `json:"spaces"`
}

func IndexToSpaceID(spaces []RawYabaiSpace) map[int]wm.SpaceID {
	ids := make(map[int]wm.SpaceID, len(spaces))
	for _, space := range spaces {
		ids[space.Index] = wm.SpaceID(strconv.Itoa(space.ID))
	}
	return ids
}

func NormalizeWindow(raw RawYabaiWindow, spaceIDByIndex map[int]wm.SpaceID) (wm.Window, error) {
	spaceID, ok := spaceIDByIndex[raw.Space]
	if !ok {
		return wm.Window{}, fmt.Errorf("window %d references unknown space index %d", raw.ID, raw.Space)
	}
	return wm.Window{
		ID: raw.ID, App: raw.App, Title: raw.Title, DisplayIdx: raw.Display,
		SpaceID: spaceID, Minimized: raw.IsMinimized, Floating: raw.IsFloating,
		Sticky: raw.IsSticky, Visible: raw.IsVisible, SplitType: raw.SplitType,
		Frame: raw.Frame,
	}, nil
}

func NormalizeWindows(rawWindows []RawYabaiWindow, rawSpaces []RawYabaiSpace) ([]wm.Window, error) {
	spaceIDs := IndexToSpaceID(rawSpaces)
	windows := make([]wm.Window, 0, len(rawWindows))
	for _, raw := range rawWindows {
		if !raw.HasAXReference {
			continue
		}
		window, err := NormalizeWindow(raw, spaceIDs)
		if err != nil {
			return nil, err
		}
		windows = append(windows, window)
	}
	return windows, nil
}

func NormalizeSpace(raw RawYabaiSpace) wm.Space {
	layout := wm.LayoutBSP
	switch raw.Type {
	case "stack":
		layout = wm.LayoutStack
	case "float":
		layout = wm.LayoutFloat
	case "bsp":
		layout = wm.LayoutBSP
	}
	return wm.Space{
		ID: wm.SpaceID(strconv.Itoa(raw.ID)), Label: raw.Label, DisplayIdx: raw.Display,
		WindowIDs: append([]int(nil), raw.Windows...), Layout: layout,
	}
}

func NormalizeDisplay(raw RawYabaiDisplay, spaceIDByIndex map[int]wm.SpaceID) (wm.Display, error) {
	spaceIDs := make([]wm.SpaceID, len(raw.Spaces))
	for index, spaceIndex := range raw.Spaces {
		spaceID, ok := spaceIDByIndex[spaceIndex]
		if !ok {
			return wm.Display{}, fmt.Errorf("display %d references unknown space index %d", raw.Index, spaceIndex)
		}
		spaceIDs[index] = spaceID
	}
	return wm.Display{Idx: raw.Index, Frame: raw.Frame, SpaceIDs: spaceIDs}, nil
}
