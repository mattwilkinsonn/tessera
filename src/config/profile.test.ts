// Contract test for the→ profile.ts port (T1). Asserts the ported
// data matches the bash source's structure and values — a porting typo (dropped
// WIN entry, wrong ratio, reordered LAPTOP_PINNED) fails here. This is the
// fidelity check for the port itself; the regex-vs-live-window re-validation
// (does each spec classify real windows as the bash engines did) is T2.

import { describe, expect, test } from "bun:test";
import { profile } from "./profile.ts";
import type { Profile } from "./types.ts";

// The engine consumes `profile` as the wide `Profile` contract (optional
// `title` present); bind it that way for the field-presence assertions.
const windows: Profile["windows"] = profile.windows;

describe("profile port fidelity", () => {
	test("DISPLAY_W widths", () => {
		expect(profile.displays).toEqual({
			g9: { width: 5120 },
			aw: { width: 3440 },
			laptop: { width: 1728 },
		});
	});

	test("all 10 WIN specs present", () => {
		expect(Object.keys(profile.windows).sort()).toEqual(
			[
				"akiflow",
				"arc",
				"discord",
				"ghostty-mbp",
				"ghostty-wave",
				"linear",
				"obsidian",
				"qalculate",
				"spotify",
				"vscode",
			].sort(),
		);
	});

	test("every WIN app matcher source matches", () => {
		// The matcher IS the port — a typo here (Code → Cody) mis-classifies
		// windows at runtime, so assert every app source against the bash values.
		const appSources = Object.fromEntries(
			Object.entries(windows).map(([name, spec]) => [name, spec.app.source]),
		);
		expect(appSources).toEqual({
			arc: "Arc",
			"ghostty-wave": "Ghostty",
			"ghostty-mbp": "Ghostty",
			vscode: "Code",
			akiflow: "Akiflow",
			obsidian: "Obsidian",
			linear: "Linear",
			spotify: "Spotify",
			discord: "Discord",
			qalculate: "Qalculate",
		});
	});

	test("the two Ghostty specs disambiguate by title", () => {
		// The latent-bug case: both are Ghostty, split only by the pc/mbp title.
		expect(windows["ghostty-wave"]?.title?.source).toBe("pc");
		expect(windows["ghostty-mbp"]?.title?.source).toBe("mbp");
		expect(windows.arc?.title).toBeUndefined();
	});

	test("COL3 ratios for 30/40/30", () => {
		expect(profile.ratios).toEqual({ col3Root: 0.3, col3Inner: 0.5714 });
	});

	test("desk labels + kinds per display", () => {
		const byDisplay = Object.fromEntries(
			profile.desk.map((d) => [d.display, d]),
		);
		expect(byDisplay.g9).toMatchObject({ label: "main", kind: "3col" });
		expect(byDisplay.aw).toMatchObject({ label: "plan", kind: "2col" });
		expect(byDisplay.laptop).toMatchObject({ label: "laptop", kind: "stack" });
	});

	test("G9 left stacks Arc + Obsidian", () => {
		const g9 = profile.desk.find((d) => d.display === "g9");
		expect(g9?.columns).toEqual([
			["arc", "obsidian"],
			["ghostty-wave"],
			["ghostty-mbp", "vscode"],
		]);
	});

	test("AW + MBP desk columns match", () => {
		const byDisplay = Object.fromEntries(
			profile.desk.map((d) => [d.display, d]),
		);
		// AW_LEFT=(linear arc), AW_RIGHT=(arc akiflow).
		expect(byDisplay.aw?.columns).toEqual([
			["linear", "arc"],
			["arc", "akiflow"],
		]);
		// MBP_STACK=(arc spotify discord qalculate) — one stacked column.
		expect(byDisplay.laptop?.columns).toEqual([
			["arc", "spotify", "discord", "qalculate"],
		]);
	});

	test("9 numpad focus slots in order, @display parsed", () => {
		// Full array: tail slots (linear/obsidian/akiflow/vscode) and the absence
		// of onDisplay on non-@ slots are as load-bearing as the AW-Arc pins.
		expect(profile.deskSlots).toEqual([
			{ name: "ghostty-wave" },
			{ name: "arc", onDisplay: "g9" },
			{ name: "ghostty-mbp" },
			{ name: "arc", onDisplay: "aw" },
			{ name: "arc", onDisplay: "aw" },
			{ name: "linear" },
			{ name: "obsidian" },
			{ name: "akiflow" },
			{ name: "vscode" },
		]);
	});

	test("LAPTOP_PINNED order with 4 repeated Arcs", () => {
		expect(profile.laptopPinned).toEqual([
			"arc",
			"ghostty-wave",
			"arc",
			"ghostty-mbp",
			"linear",
			"obsidian",
			"arc",
			"akiflow",
			"arc",
		]);
		expect(profile.laptopPinned.filter((w) => w === "arc")).toHaveLength(4);
	});

	test("LAPTOP_STACK_APPS literal app names", () => {
		expect(Object.keys(profile.laptopStackApps).sort()).toEqual(
			[
				"Messages",
				"System Settings",
				"Slack",
				"Discord",
				"1Password",
				"Activity Monitor",
			].sort(),
		);
	});
});
