package tessera

import "regexp"

#DisplayName: "g9" | "aw" | "laptop"
#Pattern:     string & regexp.Valid
#Track: [...string]
#Tracks: [...#Track]
#Weights: [...number]

#WeightTable: close({
	[=~"^[0-9]+$"]: #Weights
})

#WeightDefaults: close({
	columns?: #WeightTable
	rows?:    #WeightTable
})

#WindowSpec: close({
	app!:         #Pattern
	title?:       #Pattern
	titleInvert?: bool
	spawn?: [...string]
})

#StackLayout: close({
	display!: #DisplayName
	label!:   string
	kind!:    "stack"
	tracks!: [#Track]
})

#TrackLayout: close({
	display!: #DisplayName
	label!:   string
	kind!:    "columns" | "rows"
	tracks!:  #Tracks
	weights?: #Weights
})

#DeskLayout: #StackLayout | #TrackLayout

#DeskSlot: close({
	name!:      string
	onDisplay?: #DisplayName
})

#Topology: close({
	name!: string
	displays!: [...#DisplayName]
	desk!: [...#DeskLayout]
})

#Profile: close({
	displays!: close({
		g9!: close({
			width!:   int & >0
			weights?: #WeightDefaults
		})
		aw!: close({
			width!:   int & >0
			weights?: #WeightDefaults
		})
		laptop!: close({
			width!:   int & >0
			weights?: #WeightDefaults
		})
	})
	windows!: close({
		[string]: #WindowSpec
	})
	desk!: [...#DeskLayout]
	topologies?: [...#Topology]
	weights?: #WeightDefaults
	deskSlots!: [...#DeskSlot]
	laptopPinned!: [...string]
	laptopStackApps!: [...string]
})
