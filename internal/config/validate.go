package config

import (
	"errors"
	"fmt"
	"regexp"
)

func Validate(p *Profile) error {
	if p == nil {
		return errors.New("profile is nil")
	}

	violations := make([]error, 0, 7)
	violations = append(violations, widthViolations(p.Displays)...)
	violations = append(violations, profileWeightViolations(p)...)
	violations = append(violations, windowViolations(p.Windows)...)
	violations = append(violations, requiredProfileViolations(p)...)
	violations = append(violations, layoutViolations("desk", p.Desk, p.Windows)...)
	violations = append(violations, topologyViolations(p)...)
	violations = append(violations, referenceViolations(p)...)
	return errors.Join(violations...)
}

func widthViolations(displays Displays) []error {
	var violations []error
	for _, display := range []struct {
		name string
		spec DisplaySpec
	}{
		{name: "g9", spec: displays.G9},
		{name: "aw", spec: displays.AW},
		{name: "laptop", spec: displays.Laptop},
	} {
		if display.spec.Width <= 0 {
			violations = append(violations, fmt.Errorf("display %s: width must be greater than 0", display.name))
		}
		if display.spec.Weights != nil {
			violations = append(violations, weightDefaultViolations("display "+display.name, *display.spec.Weights)...)
		}
	}
	return violations
}

func profileWeightViolations(p *Profile) []error {
	if p.Weights == nil {
		return nil
	}
	return weightDefaultViolations("profile", *p.Weights)
}

func windowViolations(windows map[string]WindowSpec) []error {
	var violations []error
	for name, spec := range windows {
		if spec.App == "" {
			violations = append(violations, fmt.Errorf("window %q: app must not be empty", name))
		} else if appRE, err := regexp.Compile(spec.App); err != nil {
			violations = append(violations, fmt.Errorf("window %q: app pattern: %w", name, err))
		} else {
			spec.AppRE = appRE
		}
		if spec.Title == "" {
			spec.TitleRE = nil
		} else if titleRE, err := regexp.Compile(spec.Title); err != nil {
			violations = append(violations, fmt.Errorf("window %q: title pattern: %w", name, err))
		} else {
			spec.TitleRE = titleRE
		}
		windows[name] = spec
	}
	return violations
}

func requiredProfileViolations(p *Profile) []error {
	var violations []error
	if p.Windows == nil {
		violations = append(violations, errors.New("profile: windows must be set"))
	}
	if p.Desk == nil {
		violations = append(violations, errors.New("profile: desk must be set"))
	} else if len(p.Desk) == 0 {
		violations = append(violations, errors.New("profile: desk must not be empty"))
	}
	if p.DeskSlots == nil {
		violations = append(violations, errors.New("profile: deskSlots must be set"))
	}
	if p.LaptopPinned == nil {
		violations = append(violations, errors.New("profile: laptopPinned must be set"))
	}
	if p.LaptopStackApps == nil {
		violations = append(violations, errors.New("profile: laptopStackApps must be set"))
	}
	return violations
}

func layoutViolations(where string, layouts []DeskLayout, windows map[string]WindowSpec) []error {
	violations := make([]error, 0, len(layouts))
	for _, layout := range layouts {
		violations = append(violations, singleLayoutViolations(where, layout, windows)...)
	}
	return violations
}

func singleLayoutViolations(where string, layout DeskLayout, windows map[string]WindowSpec) []error {
	at := where + " " + layout.Label
	var violations []error
	if !validDisplay(layout.Display) {
		violations = append(violations, fmt.Errorf("%s: invalid display %q", at, layout.Display))
	}
	if layout.Label == "" {
		violations = append(violations, fmt.Errorf("%s: label must not be empty", where))
	}
	switch layout.Kind {
	case LayoutStack, LayoutColumns, LayoutRows:
	default:
		violations = append(violations, fmt.Errorf("%s: invalid kind %q", at, layout.Kind))
	}
	if layout.Tracks == nil {
		violations = append(violations, fmt.Errorf("%s: tracks must be set", at))
	} else if len(layout.Tracks) == 0 {
		violations = append(violations, fmt.Errorf("%s: layout must have at least one track", at))
	}
	for index, track := range layout.Tracks {
		if len(track) == 0 {
			violations = append(violations, fmt.Errorf("%s: track %d must have at least one window name", at, index))
		}
		for _, name := range track {
			if _, ok := windows[name]; !ok {
				violations = append(violations, fmt.Errorf("%s: unknown window %q", at, name))
			}
		}
	}
	violations = append(violations, layoutWeightViolations(at, layout)...)
	return violations
}

func layoutWeightViolations(at string, layout DeskLayout) []error {
	var violations []error
	if layout.Kind == LayoutStack {
		if len(layout.Tracks) != 1 {
			violations = append(violations, fmt.Errorf("%s: stack must have exactly one track", at))
		}
		if layout.Weights != nil {
			violations = append(violations, fmt.Errorf("%s: stack cannot set weights", at))
		}
	} else if len(layout.Tracks) == 1 && layout.Weights != nil {
		violations = append(violations, fmt.Errorf("%s: one-track layout cannot set weights", at))
	}
	if layout.Weights != nil {
		if len(layout.Weights) != len(layout.Tracks) {
			violations = append(violations, fmt.Errorf("%s: weights length must match tracks length", at))
		}
		violations = append(violations, weightViolations(at, layout.Weights)...)
	}
	return violations
}

func topologyViolations(p *Profile) []error {
	violations := make([]error, 0, len(p.Topologies))
	for _, topology := range p.Topologies {
		violations = append(violations, singleTopologyViolations(topology, p.Windows)...)
	}
	return violations
}

func singleTopologyViolations(topology Topology, windows map[string]WindowSpec) []error {
	where := fmt.Sprintf("topology %q", topology.Name)
	var violations []error
	if topology.Name == "" {
		violations = append(violations, errors.New("topology: name must not be empty"))
	}
	if topology.Displays == nil {
		violations = append(violations, fmt.Errorf("%s: displays must be set", where))
	}
	if topology.Desk == nil {
		violations = append(violations, fmt.Errorf("%s: desk must be set", where))
	}
	declared := make(map[DisplayName]struct{}, len(topology.Displays))
	for _, display := range topology.Displays {
		if !validDisplay(display) {
			violations = append(violations, fmt.Errorf("%s: invalid display %q", where, display))
		}
		declared[display] = struct{}{}
	}
	laidOut := make(map[DisplayName]struct{}, len(topology.Desk))
	for _, layout := range topology.Desk {
		violations = append(violations, singleLayoutViolations(where, layout, windows)...)
		if _, ok := laidOut[layout.Display]; ok {
			violations = append(violations, fmt.Errorf("%s: duplicate layout for %s", where, layout.Display))
		}
		laidOut[layout.Display] = struct{}{}
	}
	for display := range declared {
		if _, ok := laidOut[display]; !ok {
			violations = append(violations, fmt.Errorf("%s: no layout for %s", where, display))
		}
	}
	for display := range laidOut {
		if _, ok := declared[display]; !ok {
			violations = append(violations, fmt.Errorf("%s: layout for undeclared display %s", where, display))
		}
	}
	return violations
}

func referenceViolations(p *Profile) []error {
	var violations []error
	for _, slot := range p.DeskSlots {
		if slot.Name == "" {
			violations = append(violations, errors.New("desk slot: name must not be empty"))
		}
		if slot.OnDisplay != "" && !validDisplay(slot.OnDisplay) {
			violations = append(violations, fmt.Errorf("desk slot %q: invalid display %q", slot.Name, slot.OnDisplay))
		}
		if _, ok := p.Windows[slot.Name]; !ok {
			violations = append(violations, fmt.Errorf("desk slot: unknown window %q", slot.Name))
		}
	}
	for _, name := range p.LaptopPinned {
		if _, ok := p.Windows[name]; !ok {
			violations = append(violations, fmt.Errorf("laptopPinned: unknown window %q", name))
		}
	}
	return violations
}

func validDisplay(display DisplayName) bool {
	switch display {
	case DisplayG9, DisplayAW, DisplayLaptop:
		return true
	default:
		return false
	}
}
