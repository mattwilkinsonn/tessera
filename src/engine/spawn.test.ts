import { describe, expect, test } from "bun:test";
import { profile } from "../config/profile.fixture.ts";
import type { Profile } from "../config/types.ts";
import type { SpaceId, WmWindow } from "../driver/types.ts";
import { spawnsNeeded } from "./spawn.ts";

const SPACE = "s1" as SpaceId;

function win(id: number, app: string, title: string): WmWindow {
	return {
		id,
		app,
		title,
		displayIdx: 1,
		spaceId: SPACE,
		minimized: false,
		floating: false,
		sticky: false,
		visible: true,
		splitType: "none",
		frame: { x: 0, y: 0, w: 100, h: 100 },
	};
}

const spawnProfile = {
	...profile,
	windows: {
		...profile.windows,
		"ghostty-wave": {
			...profile.windows["ghostty-wave"],
			spawn: ["open", "-na", "Ghostty"],
		},
	},
} satisfies Profile;

describe("spawnsNeeded", () => {
	test("returns only names without a matching or blank candidate", () => {
		expect(
			spawnsNeeded(
				spawnProfile,
				["ghostty-wave", "ghostty-wave"],
				[win(1, "Ghostty", "blank")],
			),
		).toEqual(["ghostty-wave"]);
	});

	test("does not request a spawn when a blank app window can be claimed", () => {
		expect(
			spawnsNeeded(spawnProfile, ["ghostty-wave"], [win(1, "Ghostty", "")]),
		).toEqual([]);
	});
});
