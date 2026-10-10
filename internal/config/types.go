// Package config loads, decodes, and validates Tessera profiles.
package config

import "regexp"

type DisplayName string

const (
	DisplayG9     DisplayName = "g9"
	DisplayAW     DisplayName = "aw"
	DisplayLaptop DisplayName = "laptop"
)

type LayoutKind string

const (
	LayoutStack   LayoutKind = "stack"
	LayoutColumns LayoutKind = "columns"
	LayoutRows    LayoutKind = "rows"
)

type Profile struct {
	Displays        Displays              `json:"displays"`
	Windows         map[string]WindowSpec `json:"windows"`
	Desk            []DeskLayout          `json:"desk"`
	Topologies      []Topology            `json:"topologies,omitempty"`
	Weights         *WeightDefaults       `json:"weights,omitempty"`
	DeskSlots       []DeskSlot            `json:"deskSlots"`
	LaptopPinned    []string              `json:"laptopPinned"`
	LaptopStackApps []string              `json:"laptopStackApps"`
}

type Displays struct {
	G9     DisplaySpec `json:"g9"`
	AW     DisplaySpec `json:"aw"`
	Laptop DisplaySpec `json:"laptop"`
}

type DisplaySpec struct {
	Width   int             `json:"width"`
	Weights *WeightDefaults `json:"weights,omitempty"`
}

type WeightDefaults struct {
	Columns map[int][]float64 `json:"columns,omitempty"`
	Rows    map[int][]float64 `json:"rows,omitempty"`
}

type WindowSpec struct {
	App         string         `json:"app"`
	Title       string         `json:"title,omitempty"`
	TitleInvert bool           `json:"titleInvert,omitempty"`
	Spawn       []string       `json:"spawn,omitempty"`
	AppRE       *regexp.Regexp `json:"-"`
	TitleRE     *regexp.Regexp `json:"-"`
}

type DeskLayout struct {
	Display DisplayName `json:"display"`
	Label   string      `json:"label"`
	Kind    LayoutKind  `json:"kind"`
	Tracks  [][]string  `json:"tracks"`
	Weights []float64   `json:"weights,omitempty"`
}

type DeskSlot struct {
	Name      string      `json:"name"`
	OnDisplay DisplayName `json:"onDisplay,omitempty"`
}

type Topology struct {
	Name     string        `json:"name"`
	Displays []DisplayName `json:"displays"`
	Desk     []DeskLayout  `json:"desk"`
}
